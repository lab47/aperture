package homebrew

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	pb "github.com/schollz/progressbar/v3"
)

type Downloader struct {
	total int64
	sizes map[string]int64
}

func (d *Downloader) Test(url string) (int64, error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Authorization", "Bearer QQ==")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	return resp.ContentLength, nil
}

func downloadTo(url string, w io.Writer) (int64, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Authorization", "Bearer QQ==")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	io.Copy(w, resp.Body)

	return resp.ContentLength, nil
}

func (d *Downloader) Prep(urls []PackageURL) (int64, error) {
	d.sizes = make(map[string]int64)

	type tr struct {
		url string
		sz  int64
		err error
	}

	ch := make(chan tr, len(urls))

	var wg sync.WaitGroup

	wg.Add(len(urls))

	for _, url := range urls {
		go func(url string) {
			defer wg.Done()
			sz, err := d.Test(url)
			ch <- tr{url, sz, err}
		}(url.URL)
	}

	wg.Wait()

	close(ch)

	for {
		tr, ok := <-ch
		if !ok {
			break
		}

		d.total += tr.sz
		d.sizes[tr.url] = tr.sz
	}

	return d.total, nil
}

func (d *Downloader) Size(url string) (int64, bool) {
	sz, ok := d.sizes[url]
	return sz, ok
}

func calcPath(root, url, ext string) string {
	idx := strings.Index(url, "sha256:")
	if idx != -1 {
		return filepath.Join(root, url[idx+7:]+ext)
	}

	h := sha1.New()
	fmt.Fprintln(h, url)

	dg := hex.EncodeToString(h.Sum(nil))

	return filepath.Join(root, dg+ext)
}

func (d *Downloader) Stage(dir string, urls []PackageURL) (map[string]string, error) {
	d.sizes = make(map[string]int64)

	type tr struct {
		url  PackageURL
		path string
		err  error
	}

	ch := make(chan tr, len(urls))

	var wg sync.WaitGroup

	wg.Add(len(urls))

	if d.total == 0 {
		_, err := d.Prep(urls)
		if err != nil {
			return nil, err
		}
	}

	bar := pb.DefaultBytes(d.total, "Staging packages")

	for _, url := range urls {
		go func(url PackageURL) {
			defer wg.Done()

			path := calcPath(dir, url.URL, ".tar.gz")

			r, err := os.Open(path)
			if err == nil {
				h := sha256.New()

				io.Copy(h, r)

				fi, _ := r.Stat()
				r.Close()

				if url.Checksum.Matches(h) {
					bar.Add64(fi.Size())

					ch <- tr{url, path, err}
					return
				}
			}

			f, err := os.Create(path)
			if err != nil {
				ch <- tr{url, path, err}
				return
			}

			h := sha256.New()

			_, err = downloadTo(url.URL, io.MultiWriter(f, h, bar))
			if err != nil {
				ch <- tr{url, path, err}
			}

			if !url.Checksum.Matches(h) {
				err = fmt.Errorf("mismatched checksum of data")
			}

			ch <- tr{url, path, err}
		}(url)
	}

	wg.Wait()

	close(ch)

	out := make(map[string]string)

	var errors []string

	for {
		tr, ok := <-ch
		if !ok {
			break
		}

		if tr.err != nil {
			errors = append(errors, tr.err.Error())
		}

		out[tr.url.URL] = tr.path
	}

	var err error

	if len(errors) > 0 {
		err = fmt.Errorf("Errors staging files: %s", strings.Join(errors, ", "))
	}

	return out, err
}
