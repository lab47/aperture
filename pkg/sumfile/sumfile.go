package sumfile

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"

	"github.com/mr-tron/base58"
)

type hashedEntity struct {
	hash   []byte
	entity string
	algo   string
}

type Sumfile struct {
	entities []hashedEntity
}

func (s *Sumfile) Load(r io.Reader) error {
	br := bufio.NewReader(r)

	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		colon := bytes.IndexByte(line, ':')
		if colon == -1 {
			continue
		}

		space := bytes.IndexByte(line, ' ')
		if space == -1 {
			continue
		}

		algo := string(line[:colon])

		hash := string(line[colon+1 : space])

		entity := string(bytes.TrimSpace(line[space+1:]))

		b, err := base58.Decode(hash)
		if err != nil {
			return err
		}

		var he hashedEntity

		he.entity = entity
		he.algo = algo
		he.hash = b

		s.entities = append(s.entities, he)
	}

	return nil
}

func (s *Sumfile) Add(entity, algo string, h []byte) (string, error) {
	s.entities = append(s.entities, hashedEntity{
		algo:   algo,
		hash:   h,
		entity: entity,
	})

	sort.Slice(s.entities, func(i, j int) bool {
		return s.entities[i].entity < s.entities[j].entity
	})

	return algo + ":" + base58.Encode(h), nil
}

func (s *Sumfile) Save(w io.Writer) error {
	for _, he := range s.entities {
		sh := base58.Encode(he.hash)
		fmt.Fprintf(w, "%s:%s %s\n", he.algo, sh, he.entity)
	}

	return nil
}

func (s *Sumfile) Lookup(entity string) (string, []byte, bool) {
	idx := sort.Search(len(s.entities), func(i int) bool {
		return s.entities[i].entity >= entity
	})

	if idx == len(s.entities) {
		return "", nil, false
	}

	if s.entities[idx].entity == entity {
		return s.entities[idx].algo, s.entities[idx].hash, true
	}

	return "", nil, false
}
