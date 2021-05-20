package evt

import (
	"regexp"
	"strings"
)

type FSPath string

type EVTNode interface {
	evtNode()
}

type Statements struct {
	Statements []EVTNode
}

type System struct {
	Arguments []string
	Dir       FSPath
}

type SetRoot struct {
	Dir FSPath
}

type ChangeDir struct {
	Dir  FSPath
	Body EVTNode
}

type MakeDir struct {
	Dir FSPath
}

type Shell struct {
	Code string
}

type Patch struct {
	Patch string
}

type Replace struct {
	File     FSPath
	Replacer *strings.Replacer
	Regexp   *regexp.Regexp
	Target   []byte
}

type Rmrf struct {
	Target string
}

type SetEnv struct {
	Append  bool
	Prepend bool
	Key     string
	Value   string
}

type Link struct {
	Original FSPath
	Target   FSPath
}

type Unpack struct {
	Path   FSPath
	Output FSPath
}

type KnownSum struct {
	Type  string
	Value string
}

type Download struct {
	URL  string
	Path FSPath
	Sum  *KnownSum
}

type InstallFiles struct {
	Target  FSPath
	Pattern FSPath
	Symlink bool
}

type WriteFile struct {
	Target FSPath
	Data   []byte
}

func (s *Statements) evtNode()   {}
func (s *System) evtNode()       {}
func (s *SetRoot) evtNode()      {}
func (s *ChangeDir) evtNode()    {}
func (s *MakeDir) evtNode()      {}
func (s *Shell) evtNode()        {}
func (s *Patch) evtNode()        {}
func (s *Replace) evtNode()      {}
func (s *Rmrf) evtNode()         {}
func (s *SetEnv) evtNode()       {}
func (s *Link) evtNode()         {}
func (s *Unpack) evtNode()       {}
func (s *Download) evtNode()     {}
func (s *InstallFiles) evtNode() {}
func (s *WriteFile) evtNode()    {}
