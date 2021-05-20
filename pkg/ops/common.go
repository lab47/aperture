package ops

import "github.com/hashicorp/go-hclog"

type common struct {
	logger hclog.Logger
}

func (c *common) L() hclog.Logger {
	if c.logger != nil {
		return c.logger
	}

	c.logger = hclog.L()

	return c.logger
}

func (c *common) SetLogger(logger hclog.Logger) {
	c.logger = logger
}
