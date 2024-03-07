package cmd

import (
	"net/http"
)

type LoggingTransport struct {
	Transport http.RoundTripper
}

func (t *LoggingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	Info("curl '%s' -H 'Authorization: %s'", request.URL, "ghp_******")
	return t.Transport.RoundTrip(request)
}
