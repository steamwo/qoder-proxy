package qoder

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// UpstreamError is a Qoder business-layer rejection discovered inside an
// otherwise-successful HTTP/SSE response.
type UpstreamError struct {
	HTTPStatus  int
	QoderStatus int
	QoderCode   string
	PublicCode  string
	Type        string
	Message     string
}

func (e *UpstreamError) Error() string {
	if e == nil {
		return ""
	}
	if e.QoderCode != "" {
		return fmt.Sprintf("Qoder rejected request: status %d code %s: %s", e.QoderStatus, e.QoderCode, e.Message)
	}
	return fmt.Sprintf("Qoder rejected request: status %d: %s", e.QoderStatus, e.Message)
}

func upstreamErrorFromEnvelope(data string) *UpstreamError {
	var envelope map[string]any
	if json.Unmarshal([]byte(data), &envelope) != nil {
		return nil
	}
	status := numberAsInt(envelope["statusCodeValue"])
	if status == 0 {
		status = http.StatusOK
	}
	if status == http.StatusOK {
		return nil
	}
	inner, _ := envelopeBodyText(envelope)
	code, msg := qoderErrorDetails(inner, status)
	return classifyUpstreamError(status, code, msg)
}

func classifyUpstreamError(status int, code, msg string) *UpstreamError {
	e := &UpstreamError{
		HTTPStatus:  status,
		QoderStatus: status,
		QoderCode:   code,
		PublicCode:  code,
		Type:        "upstream_error",
		Message:     strings.TrimSpace(msg),
	}
	if e.HTTPStatus < 400 || e.HTTPStatus > 599 {
		e.HTTPStatus = http.StatusBadGateway
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("Qoder status %d", status)
	}

	switch code {
	case "112":
		// Qoder returns code 112 with a pricingUrl when the account has no
		// usable credits for the requested model. Map it to the OpenAI-style
		// insufficient_quota error so clients handle it as a quota failure.
		e.HTTPStatus = http.StatusTooManyRequests
		e.PublicCode = "insufficient_quota"
		e.Type = "insufficient_quota"
		if url := findNestedString(e.Message, "pricingUrl"); url != "" {
			e.Message = "Qoder account has no available quota for this request. Pricing: " + url
		} else {
			e.Message = "Qoder account has no available quota for this request"
		}
	default:
		switch status {
		case http.StatusUnauthorized:
			e.Type = "authentication_error"
			if e.PublicCode == "" {
				e.PublicCode = "authentication_error"
			}
		case http.StatusForbidden:
			e.Type = "permission_error"
			if e.PublicCode == "" {
				e.PublicCode = "permission_denied"
			}
		case http.StatusTooManyRequests:
			e.Type = "rate_limit_error"
			if e.PublicCode == "" {
				e.PublicCode = "rate_limit_exceeded"
			}
		}
		if e.PublicCode == "" {
			e.PublicCode = "qoder_upstream_error"
		}
	}
	return e
}

func findNestedString(input, key string) string {
	for _, m := range nestedJSONMaps(input, 5) {
		if v := stringValue(m[key]); v != "" {
			return v
		}
	}
	return ""
}
