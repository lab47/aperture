package cleanhttp

import (
	"net"
	"net/http"
	"time"
)

var DefaultTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	DisableCompression:    true,
}

var DefaultClient = &http.Client{
	Transport: DefaultTransport,
}

func Get(url string) (resp *http.Response, err error) {
	return DefaultClient.Get(url)
}

func Do(req *http.Request) (resp *http.Response, err error) {
	return DefaultClient.Do(req)
}
