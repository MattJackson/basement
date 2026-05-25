package store

import "testing"

func TestConnection_Redacted(t *testing.T) {
	c := Connection{
		ID: "x", Label: "lab", Driver: "garage",
		Config: map[string]string{
			"admin_url":   "http://example",
			"admin_token": "SECRET",
			"secret_key":  "AWS-SECRET",
			"s3_endpoint": "http://s3",
			"auth_token":  "BEARER-SECRET",
		},
	}
	r := c.Redacted()
	for _, k := range []string{"admin_token", "secret_key", "auth_token"} {
		if _, ok := r.Config[k]; ok {
			t.Errorf("Redacted() leaked %q", k)
		}
	}
	for _, k := range []string{"admin_url", "s3_endpoint"} {
		if _, ok := r.Config[k]; !ok {
			t.Errorf("Redacted() dropped public key %q", k)
		}
	}
	if _, ok := c.Config["admin_token"]; !ok {
		t.Errorf("Redacted() mutated the original Config map")
	}
}
