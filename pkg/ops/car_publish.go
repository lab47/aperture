package ops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/pkg/errors"
	"lab47.dev/aperture/pkg/data"
)

type CarPublish struct {
}

func (c *CarPublish) getInfo(path string) (*data.CarInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err != nil {
			return nil, err
		}

		if hdr.Name == CarInfoJson {
			var info data.CarInfo

			err = json.NewDecoder(tr).Decode(&info)
			if err != nil {
				return nil, err
			}

			return &info, nil
		}
	}
}

func (c *CarPublish) PublishCar(path, repo string) error {
	info, err := c.getInfo(path)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/%s-%s:%s", repo, info.Name, info.Version, info.ID)

	ref, err := name.ParseReference(target)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}

	defer f.Close()

	var (
		cf  v1.ConfigFile
		man v1.Manifest
		img ociImage
	)

	img.layer = &ociLayer{f: f, man: &man}
	img.config = &cf

	data, err := json.Marshal(&cf)
	if err != nil {
		return err
	}

	img.configData = data

	digest, sz, err := v1.SHA256(f)
	if err != nil {
		return err
	}

	f.Seek(0, os.SEEK_SET)

	man.Layers = append(man.Layers, v1.Descriptor{
		MediaType: types.OCILayer,
		Size:      int64(sz),
		Digest:    digest,
		Annotations: map[string]string{
			"org.opencontainers.image.title": path,
		},
	})

	img.layer.digest = &digest
	img.layer.size = int64(sz)

	h, n, err := v1.SHA256(bytes.NewReader(img.configData))
	if err != nil {
		return err
	}

	idx := strings.IndexByte(info.ID, '-')

	hashRef := info.ID[:idx]

	man.MediaType = types.OCIManifestSchema1
	man.SchemaVersion = 2
	man.Annotations = map[string]string{
		"com.github.package.type":              "aperture-package",
		"org.opencontainers.image.description": "Aperture Package",
		"org.opencontainers.image.ref.name":    info.ID,
		"org.opencontainers.image.revision":    hashRef,
		"org.opencontainers.image.source":      info.Repo,
		"org.opencontainers.image.title":       info.Name + "-" + info.Version,
		"org.opencontainers.image.vendor":      "lab47",
		"org.opencontainers.image.version":     info.Version,
	}
	man.Config.Digest = h
	man.Config.MediaType = types.OCIConfigJSON
	man.Config.Size = n

	data, err = json.Marshal(&man)
	if err != nil {
		return err
	}

	img.manifest = &man
	img.manifestData = data

	return remote.Write(ref, &img,
		remote.WithJobs(1),
		remote.WithAuth(&authn.Basic{
			Username: "evanphx",
			Password: "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",
		}))

	/*

		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			panic(err)
		}

		destRef, err := alltransports.ParseImageName(target)
		if err != nil {
			return err
		}

		var sc types.SystemContext

		ctx := context.Background()

		idest, err := destRef.NewImageDestination(ctx, &sc)
		if err != nil {
			return err
		}

		var inputInfo types.BlobInfo

		f, err := os.Open(path)
		if err != nil {
			return err
		}

		defer f.Close()

		blobInfo, err := idest.PutBlob(ctx, f, inputInfo, nil, true)
		if err != nil {
			return err
		}

		var desc v1.Descriptor

		desc.Annotations = info.Constraints

		manifest := manifest.OCI1FromComponents(desc, nil)

		err = manifest.UpdateLayerInfos([]types.BlobInfo{blobInfo})
		if err != nil {
			return err
		}

		manData, err := manifest.Serialize()
		if err != nil {
			return err
		}

		err = idest.PutManifest(ctx, manData, nil)
		if err != nil {
			return err
		}

		return idest.Commit(ctx, nil)
	*/
}

// ociLayer is a streaming implementation of v1.Layer.
type ociLayer struct {
	f        io.ReadCloser
	consumed bool
	digest   *v1.Hash
	size     int64
	man      *v1.Manifest
}

var _ v1.Layer = (*ociLayer)(nil)

// Digest implements v1.Layer.
func (l *ociLayer) Digest() (v1.Hash, error) {
	if l.digest == nil {
		return v1.Hash{}, stream.ErrNotComputed
	}
	return *l.digest, nil
}

// DiffID implements v1.Layer.
func (l *ociLayer) DiffID() (v1.Hash, error) {
	return v1.Hash{}, stream.ErrNotComputed
}

// Size implements v1.Layer.
func (l *ociLayer) Size() (int64, error) {
	if l.size == 0 {
		return 0, stream.ErrNotComputed
	}
	return l.size, nil
}

// MediaType implements v1.Layer
func (l *ociLayer) MediaType() (types.MediaType, error) {
	return types.OCILayer, nil
}

// Uncompressed implements v1.Layer.
func (l *ociLayer) Uncompressed() (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

// Compressed implements v1.Layer.
func (l *ociLayer) Compressed() (io.ReadCloser, error) {
	if l.consumed {
		return nil, stream.ErrConsumed
	}
	return &trackReader{h: sha256.New(), l: l}, nil
}

type trackReader struct {
	h hash.Hash
	l *ociLayer
}

func (cr *trackReader) Read(b []byte) (int, error) {
	sz, err := cr.l.f.Read(b)
	cr.l.size += int64(sz)
	cr.h.Write(b[:sz])

	return sz, err
}

func (cr *trackReader) Close() error {
	digest, err := v1.NewHash("sha256:" + hex.EncodeToString(cr.h.Sum(nil)))
	if err != nil {
		return err
	}
	cr.l.digest = &digest

	fmt.Printf("layer uploaded: %d => %s", cr.l.size, digest.String())

	cr.l.consumed = true
	return nil
}

type ociImage struct {
	layer  *ociLayer
	config *v1.ConfigFile

	configData   []byte
	manifest     *v1.Manifest
	manifestData []byte
}

// Layers returns the ordered collection of filesystem layers that comprise this image.
// The order of the list is oldest/base layer first, and most-recent/top layer last.
func (o *ociImage) Layers() ([]v1.Layer, error) {
	return []v1.Layer{o.layer}, nil
}

// MediaType of this image's manifest.
func (o *ociImage) MediaType() (types.MediaType, error) {
	return types.OCIManifestSchema1, nil
}

// Size returns the size of the manifest.
func (o *ociImage) Size() (int64, error) {
	return int64(len(o.manifestData)), nil
}

// ConfigName returns the hash of the image's config file, also known as
// the Image ID.
func (o *ociImage) ConfigName() (v1.Hash, error) {
	h, _, err := v1.SHA256(bytes.NewReader(o.configData))
	return h, err
}

// ConfigFile returns this image's config file.
func (o *ociImage) ConfigFile() (*v1.ConfigFile, error) {
	return o.config, nil
}

// RawConfigFile returns the serialized bytes of ConfigFile().
func (o *ociImage) RawConfigFile() ([]byte, error) {
	return o.configData, nil
}

// Digest returns the sha256 of this image's manifest.
func (o *ociImage) Digest() (v1.Hash, error) {
	h, _, err := v1.SHA256(bytes.NewReader(o.manifestData))
	return h, err
}

// Manifest returns this image's Manifest object.
func (o *ociImage) Manifest() (*v1.Manifest, error) {
	return o.manifest, nil
}

// RawManifest returns the serialized bytes of Manifest()
func (o *ociImage) RawManifest() ([]byte, error) {
	return o.manifestData, nil
}

// LayerByDigest returns a Layer for interacting with a particular layer of
// the image, looking it up by "digest" (the compressed hash).
func (o *ociImage) LayerByDigest(_ v1.Hash) (v1.Layer, error) {
	panic("not implemented") // TODO: Implement
}

// LayerByDiffID is an analog to LayerByDigest, looking up by "diff id"
// (the uncompressed hash).
func (o *ociImage) LayerByDiffID(_ v1.Hash) (v1.Layer, error) {
	panic("not implemented") // TODO: Implement
}
