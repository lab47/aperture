package config

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mitchellh/go-homedir"
	"github.com/mr-tron/base58"
	"github.com/shirou/gopsutil/v3/host"
	"lab47.dev/aperture/pkg/metadata"
	"lab47.dev/aperture/pkg/repo"
)

type EDSigner interface {
	Public() ed25519.PublicKey
	Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error)
}

type Config struct {
	path      string
	configDir string

	mu sync.Mutex

	signer   crypto.Signer
	signerId string
	pubKey   ed25519.PublicKey
	privKey  ed25519.PrivateKey

	// Actual Config
	DataDir      string `json:"data-dir"`
	Path         string `json:"chell-path"`
	ProfilesPath string `json:"profiles-path"`
	Profile      string `json:"profile"`
}

const (
	DefaultConfigPath   = "~/.config/iris/config.json"
	DefaultProfilesPath = "~/.config/iris/profiles"
	DefaultProfile      = "main"
	DefaultDataDir      = "/opt/iris"
	DefaultPath         = "github.com/lab47/aperture-packages"
)

func LoadConfig() (*Config, error) {
	if loc := os.Getenv("APERTURE_CONFIG"); loc != "" {
		return loadFile(loc)
	}

	path, err := homedir.Expand(DefaultConfigPath)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); err == nil {
		return loadFile(path)
	}

	dir := filepath.Dir(path)

	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, err
	}

	ppath, err := homedir.Expand(DefaultProfilesPath)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		path:      path,
		configDir: dir,

		DataDir:      DefaultDataDir,
		Path:         DefaultPath,
		ProfilesPath: ppath,
		Profile:      DefaultProfile,
	}

	return updateFromEnv(cfg)
}

func loadFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	var cfg Config

	err = json.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return nil, err
	}

	cfg.path = path
	cfg.configDir = filepath.Dir(path)

	if cfg.DataDir == "" {
		cfg.DataDir = DefaultConfigPath
	}

	if cfg.Path == "" {
		cfg.Path = DefaultPath
	}

	if cfg.ProfilesPath == "" {
		ppath, err := homedir.Expand(DefaultProfilesPath)
		if err != nil {
			return nil, err
		}

		cfg.ProfilesPath = ppath
	}

	if cfg.Profile == "" {
		cfg.Profile = DefaultProfile
	}

	return updateFromEnv(&cfg)
}

func updateFromEnv(cfg *Config) (*Config, error) {
	if path := os.Getenv("APERTURE_DATA_DIR"); path != "" && path != DefaultPath {
		fi, err := os.Stat(path)
		if err != nil {
			return nil, err
		}

		if !fi.IsDir() {
			return nil, fmt.Errorf("path is not a directory: %s", path)
		}

		cfg.DataDir = path
	}

	if path := os.Getenv("APERTURE_PATH"); path != "" {
		cfg.Path = path
	}

	if path := os.Getenv("APERTURE_PROFILES"); path != "" {
		cfg.ProfilesPath = path
	}

	if name := os.Getenv("APERTURE_PROFILE"); name != "" {
		cfg.Profile = name
	}

	return ensureDirs(cfg)
}

func ensureDirs(cfg *Config) (*Config, error) {
	dirs := []string{
		cfg.DataDir,
		cfg.ProfilesPath,
		filepath.Join(cfg.ProfilesPath, cfg.Profile),
		filepath.Join(cfg.RootsPath()),
	}

	for _, dir := range dirs {
		fi, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				err = os.MkdirAll(dir, 0755)
				if err != nil {
					return nil, err
				}
			}
		} else if !fi.IsDir() {
			return nil, fmt.Errorf("path is not a directory: %s", dir)
		}
	}

	current := filepath.Join(cfg.ProfilesPath, "current")
	if _, err := os.Stat(current); err != nil {
		if os.IsNotExist(err) {
			err = os.Symlink(filepath.Join(cfg.ProfilesPath, cfg.Profile), current)
			if err != nil {
				return nil, err
			}
		}
	}

	return cfg, nil
}

func (c *Config) ensureSignerSet() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.signer != nil {
		return nil
	}

	var (
		signer   crypto.Signer
		priv     ed25519.PrivateKey
		pub      ed25519.PublicKey
		signerId string
	)

	path := filepath.Join(c.configDir, "key")

	if data, err := ioutil.ReadFile(path); err == nil {
		data, err = base58.Decode(string(data))
		if err != nil {
			return err
		}

		priv = ed25519.PrivateKey(data)
		pub = priv.Public().(ed25519.PublicKey)
		signerId = "1:" + base58.Encode(priv.Public().(ed25519.PublicKey))
		signer = priv

	} else {
		epub, epriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}

		pub = epub
		priv = epriv

		err = ioutil.WriteFile(path, []byte(base58.Encode(epriv)), 0600)
		if err != nil {
			return err
		}

		signerId = "1:" + base58.Encode(pub)
		signer = epriv
	}

	c.signer = signer
	c.signerId = signerId
	c.pubKey = pub
	c.privKey = priv

	return nil
}

func (c *Config) SignerId() (string, error) {
	if err := c.ensureSignerSet(); err != nil {
		return "", nil
	}

	return c.signerId, nil
}

func (c *Config) Public() ed25519.PublicKey {
	if err := c.ensureSignerSet(); err != nil {
		return nil
	}

	return c.pubKey
}

func (c *Config) Private() ed25519.PrivateKey {
	if err := c.ensureSignerSet(); err != nil {
		return nil
	}

	return c.privKey
}

func (c *Config) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	if err := c.ensureSignerSet(); err != nil {
		return nil, nil
	}

	return c.signer.Sign(rand, digest, opts)
}

func (c *Config) StorePath() string {
	return filepath.Join(c.DataDir, "store")
}

func (c *Config) RootsPath() string {
	return filepath.Join(c.DataDir, "roots")
}

func (c *Config) GlobalProfilePath() string {
	return filepath.Join(c.ProfilesPath, c.Profile)
}

type PathPart struct {
	Name string
	Path string
}

func (c *Config) NamedPath() []PathPart {
	var pp []PathPart

	for _, c := range strings.Split(c.Path, ":") {
		idx := strings.IndexByte(c, '=')
		if idx == -1 {
			pp = append(pp, PathPart{Path: c})
		} else {
			pp = append(pp, PathPart{
				Name: c[:idx],
				Path: c[idx+1:],
			})
		}
	}

	return pp
}

func (c *Config) LoadPath() []string {
	var pp []string

	for _, c := range strings.Split(c.Path, ":") {
		idx := strings.IndexByte(c, '=')
		if idx == -1 {
			pp = append(pp, c)
		} else {
			pp = append(pp, c[idx+1:])
		}
	}

	return pp
}

func (c *Config) Repo() repo.Repo {
	return &ConfigRepo{c}
}

type ConfigRepo struct {
	c *Config
}

func (c *ConfigRepo) Lookup(name string) (repo.Entry, error) {
	for _, part := range c.c.NamedPath() {
		r, err := repo.Open(part.Path)
		if err != nil {
			return nil, err
		}

		ent, err := r.Lookup(name)
		if err != nil {
			if err == repo.ErrNotFound {
				continue
			}

			return nil, err
		}

		return ent, nil
	}

	return nil, repo.ErrNotFound
}

func (c *ConfigRepo) Config() (*metadata.RepoConfig, error) {
	for _, part := range c.c.NamedPath() {
		r, err := repo.Open(part.Path)
		if err != nil {
			return nil, err
		}

		return r.Config()
	}

	return nil, nil
}

func (c *Config) Constraints() map[string]string {
	constraints := SystemConstraints()
	constraints["aperture/root"] = c.DataDir

	return constraints
}

func Platform() (string, string, string) {
	osName, _, osVersion, err := host.PlatformInformation()
	if err != nil {
		panic(err)
	}

	arch, err := host.KernelArch()
	if err != nil {
		panic(err)
	}

	return osName, osVersion, arch
}

func SystemConstraints() map[string]string {
	osName, osVersion, arch := Platform()

	constraints := map[string]string{
		"machine/arch": arch,
		"os/name":      osName,
	}

	if osName == "darwin" {
		// Strip off the minor version
		dot := strings.LastIndexByte(osVersion, '.')
		if dot != -1 {
			osVersion = osVersion[:dot]
		}

		constraints["darwin/version"] = osVersion
	}

	return constraints
}
