package humanize

func Size(i int64) (float64, string) {
	switch {
	case i < 1024:
		return float64(i), "B"
	case i < 1024*1024:
		return float64(i) / 1024, "KB"
	case i < 1024*1024*1024:
		return float64(i) / (1024 * 1024), "MB"
	default:
		return float64(i) / (1024 * 1024 * 1024), "GB"
	}
}
