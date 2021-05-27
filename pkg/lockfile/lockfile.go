package lockfile

import (
	"context"
	"os"
	"time"
)

func Take(ctx context.Context, path string, waiting func()) (func(), error) {
	tk := time.NewTicker(time.Second)
	defer tk.Stop()

	var (
		f   *os.File
		err error
	)

	for {
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			break
		}

		if waiting != nil {
			waiting()
		}

		select {
		case <-tk.C:
			// ok
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	f.Close()

	closer := func() {
		os.Remove(path)
	}

	return closer, nil
}
