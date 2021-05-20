package ops

import "github.com/pkg/errors"

func track(err error) error {
	return errors.WithStack(err)
}
