package ops

import (
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/exprcore/exprcore"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
	"lab47.dev/aperture/pkg/evt"
	"lab47.dev/aperture/pkg/lang"
)

type ScriptInput struct {
	Name     string
	Data     *ScriptFile
	Instance *Instance
}

type ScriptCalcSig struct {
	common

	Name         string
	Version      string
	Description  string
	URL          string
	Metadata     map[string]string
	Install      *exprcore.Function
	Hook         *exprcore.Function
	PostInstall  *exprcore.Function
	Inputs       []ScriptInput
	Dependencies []*ScriptPackage
	ExplicitDeps []*ScriptPackage
	Instances    []*Instance
}

func exprString(val exprcore.Value) string {
	switch v := val.(type) {
	case exprcore.String:
		return string(v)
	default:
		return v.String()
	}
}

func (s *ScriptCalcSig) extract(proto *exprcore.Prototype) error {
	name, err := lang.StringValue(proto.Attr("name"))
	if err != nil {
		return err
	}

	s.Name = name

	ver, err := lang.StringValue(proto.Attr("version"))
	if err != nil {
		return err
	}

	if ver == "" {
		ver = "unknown"
	}

	s.Version = ver

	s.Description, err = lang.StringValue(proto.Attr("description"))
	if err != nil {
		return err
	}

	s.URL, err = lang.StringValue(proto.Attr("url"))
	if err != nil {
		return err
	}

	val, err := proto.Attr("metadata")
	if err == nil {
		m, ok := val.(exprcore.IterableMapping)
		if ok {
			metadata := map[string]string{}

			for _, items := range m.Items() {
				metadata[exprString(items[0])] = exprString(items[1])
			}

			s.Metadata = metadata
		}
	}

	val, err = proto.Attr("input")
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); ok {
			val = nil
		} else {
			return err
		}
	}

	if val != nil {
		err = s.processInput(val)
		if err != nil {
			return err
		}
	}

	install, err := lang.FuncValue(proto.Attr("install"))
	if err != nil {
		return err
	}

	s.Install = install

	hook, err := lang.FuncValue(proto.Attr("hook"))
	if err != nil {
		return err
	}

	s.Hook = hook

	post, err := lang.FuncValue(proto.Attr("post_install"))
	if err != nil {
		return err
	}

	s.PostInstall = post

	depSet := map[string]struct{}{}

	deps, err := lang.ListValue(proto.Attr("dependencies"))
	if err != nil {
		return err
	}

	if deps != nil {
		var scripts []*ScriptPackage

		iter := deps.Iterate()
		defer iter.Done()
		var x exprcore.Value
		for iter.Next(&x) {
			if script, ok := x.(*ScriptPackage); ok {
				depSet[script.ID()] = struct{}{}
				scripts = append(scripts, script)
			} else {
				fmt.Printf("unknown return type: %T", x)
			}
		}

		s.Dependencies = scripts
	}

	deps, err = lang.ListValue(proto.Attr("explicit_dependencies"))
	if err != nil {
		return err
	}

	if deps != nil {
		var scripts []*ScriptPackage

		iter := deps.Iterate()
		defer iter.Done()
		var x exprcore.Value
		for iter.Next(&x) {
			if script, ok := x.(*ScriptPackage); ok {
				scripts = append(scripts, script)
				if _, ok := depSet[script.ID()]; !ok {
					s.Dependencies = append(s.Dependencies, script)
				}
			} else {
				fmt.Printf("unknown return type: %T", x)
			}
		}

		s.ExplicitDeps = scripts
	}

	return nil
}

func (s *ScriptCalcSig) processInput(val exprcore.Value) error {
	var inputs []ScriptInput

	switch v := val.(type) {
	case *ScriptFile:
		inputs = append(inputs, ScriptInput{
			Name: "source",
			Data: v,
		})
	case *Instance:
		inputs = append(inputs, ScriptInput{
			Name:     "source",
			Instance: v,
		})
	case *exprcore.Dict:
		for _, i := range v.Items() {
			key, ok := i.Index(0).(exprcore.String)
			if !ok {
				return fmt.Errorf("key not a string")
			}

			dv := i.Index(1)

			switch f := dv.(type) {
			case *ScriptFile:
				inputs = append(inputs, ScriptInput{
					Name: string(key),
					Data: f,
				})
			case *Instance:
				inputs = append(inputs, ScriptInput{
					Name:     string(key),
					Instance: f,
				})
			default:
				return fmt.Errorf("unsupported type in inputs: %T", dv)
			}
		}
	default:
		return fmt.Errorf("unsupported type in inputs: %T", val)
	}

	sort.Slice(inputs, func(i, j int) bool {
		return inputs[i].Name < inputs[j].Name
	})

	s.Inputs = inputs

	return nil
}

type calcLogger struct {
	logger hclog.Logger
	h      hash.Hash
}

func (c *calcLogger) Write(b []byte) (int, error) {
	c.h.Write(b)

	s := strconv.QuoteToASCII(string(b))

	/*
		for _, r := range s {
			if !unicode.IsPrint(r) {
				c.logger.Debug("calc-part", "part", b)
				return len(b), nil
			}
		}
	*/

	c.logger.Debug("calc-part", "part", s[1:len(s)-1], "sum", hex.EncodeToString(c.h.Sum(nil)))
	return len(b), nil
}

type sigDataInstance struct {
	_         struct{} `hash:"instance"`
	Name      string
	Version   string
	Signature string
}

type sigData struct {
	_            struct{} `hash:"signature"`
	Name         string
	Version      string
	Constraints  map[string]string
	Instances    map[string]struct{}
	FuncSig      string
	PostSig      string
	Dependencies map[string]struct{}
}

func (s *ScriptCalcSig) calcSig(
	proto *exprcore.Prototype,
	helperSum []byte,
	constraints map[string]string,
) (string, error) {
	if s.Name == "" {
		err := s.extract(proto)
		if err != nil {
			return "", err
		}
	}

	sd := sigData{
		Name:        s.Name,
		Version:     s.Version,
		Constraints: constraints,
	}

	if s.Inputs != nil {
		instances, err := s.injectInputs()
		if err != nil {
			return "", err
		}

		s.Instances = instances

		sd.Instances = map[string]struct{}{}

		for _, i := range instances {
			val := fmt.Sprintf("%s-%s-%s", i.Name, i.Version, i.Signature)
			sd.Instances[val] = struct{}{}
		}
	}

	if s.Install != nil {
		funcSig, err := s.calcFuncSig(s.Install)
		if err != nil {
			return "", err
		}

		sd.FuncSig = funcSig
	}

	if s.PostInstall != nil {
		funcSig, err := s.calcFuncSig(s.PostInstall)
		if err != nil {
			return "", err
		}

		sd.PostSig = funcSig
	}

	sd.Dependencies = make(map[string]struct{})

	for _, scr := range s.Dependencies {
		sd.Dependencies[scr.ID()] = struct{}{}
	}

	if len(s.ExplicitDeps) > 0 {
		for _, scr := range s.ExplicitDeps {
			sd.Dependencies[scr.ID()] = struct{}{}
		}
	}

	hb, _ := blake2b.New256(nil)

	h := &calcLogger{logger: s.L(), h: hb}

	err := evt.HashInto(&sd, h)
	if err != nil {
		return "", err
	}

	return base58.Encode(hb.Sum(nil)), nil
}

func (s *ScriptCalcSig) calcFuncSig(fn exprcore.Value) (string, error) {
	var rc RunCtx
	rc.attrs = RunCtxFunctions
	rc.topDir = "$top"
	rc.buildDir = "$build"
	rc.installDir = "$prefix"
	rc.stateDir = "$state"

	h, _ := blake2b.New256(nil)

	rc.h = h

	args := exprcore.Tuple{&rc}

	var thread exprcore.Thread

	_, err := exprcore.Call(&thread, fn, args, nil)
	if err != nil {
		return "", err
	}

	return base58.Encode(h.Sum(nil)), nil
}

func (s *ScriptCalcSig) calcInstance(inst *Instance) error {
	funcSig, err := s.calcFuncSig(inst.Fn)
	if err != nil {
		return err
	}

	inst.Signature = funcSig

	return nil
}

func (s *ScriptCalcSig) injectInputs() ([]*Instance, error) {
	var instances []*Instance

	for _, i := range s.Inputs {
		if i.Instance != nil {
			if i.Instance.Signature == "" {
				err := s.calcInstance(i.Instance)
				if err != nil {
					return nil, err
				}
			}

			instances = append(instances, i.Instance)
			continue
		}

		spew.Dump(i)
		panic("not supported")
	}

	return instances, nil
}

func (s *ScriptCalcSig) hashPath(sf *ScriptFile, data ScriptData) (string, []byte, bool) {
	path := sf.path

	if kt, kv, ok := sf.Sum(); ok {
		return kt, kv, true
	}

	h, _ := blake2b.New256(nil)

	u, err := url.Parse(path)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		resp, err := http.Head(path)
		if err != nil {
			return "", nil, false
		}

		defer resp.Body.Close()

		if etag := resp.Header.Get("Etag"); etag != "" && etag[0] == '"' {
			return "etag", []byte(etag), true
		}

		resp, err = http.Get(path)
		if err != nil {
			return "", nil, false
		}

		defer resp.Body.Close()

		io.Copy(h, resp.Body)
	} else {
		ad, err := data.Asset(path)
		if err != nil {
			return "", nil, false
		}

		_, err = h.Write(ad)
		if err != nil {
			return "", nil, false
		}
	}

	return "b2", h.Sum(nil), true
}

var times int

func (s *ScriptCalcSig) Calculate(
	proto *exprcore.Prototype,
	helperSum []byte,
	constraints map[string]string,
) (string, string, error) {
	sig, err := s.calcSig(proto, helperSum, constraints)
	if err != nil {
		return "", "", err
	}

	return sig, fmt.Sprintf("%s-%s-%s", sig, s.Name, s.Version), nil
}
