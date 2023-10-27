package cmd

import (
	"log"
	"net/http"
)

type LoggingTransport struct {
	Transport http.RoundTripper
}

func (t *LoggingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	log.Printf("curl '%s' -H 'Authorization: %s'", request.URL, "ghp_******")
	return t.Transport.RoundTrip(request)
}
