package progress

import (
	"context"
	"fmt"
	"io"
	"time"

	pb "github.com/schollz/progressbar/v3"
)

type pbVal struct {
	w io.Writer
}

type pbKey struct{}

func Open(ctx context.Context, w io.Writer) context.Context {
	return context.WithValue(ctx, pbKey{}, pbVal{w})
}

type Progress struct {
	bar    *pb.ProgressBar
	prefix string
}

func (t *Progress) Add(cnt int64) {
	if t.bar == nil {
		return
	}

	t.bar.Add64(cnt)
}

func (t *Progress) Tick() {
	t.Add(1)
}

func (t *Progress) Close() {
	if t.bar == nil {
		return
	}

	t.bar.Close()
}

func (t *Progress) On(step string) {
	if t.bar == nil {
		return
	}

	t.bar.Describe(t.prefix + ": " + step)
}

func Count(ctx context.Context, total int64, desc string) *Progress {
	h := ctx.Value(pbKey{})
	if h == nil {
		return &Progress{}
	}

	val := h.(pbVal)

	bar := pb.NewOptions64(
		total,
		pb.OptionSetDescription(desc),
		pb.OptionSetWriter(val.w),
		pb.OptionSetWidth(20),
		pb.OptionThrottle(65*time.Millisecond),
		pb.OptionShowCount(),
		pb.OptionShowIts(),
		pb.OptionSetTheme(
			pb.Theme{Saucer: "=", SaucerPadding: " ", BarStart: "[", BarEnd: "]"},
		),
		pb.OptionOnCompletion(func() {
			fmt.Fprint(val.w, "\n")
		}),
		pb.OptionSpinnerType(14),
		pb.OptionFullWidth(),
	)
	bar.RenderBlank()

	return &Progress{prefix: desc, bar: bar}
}
