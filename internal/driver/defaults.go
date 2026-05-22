package driver

// EndpointDefaults are hints for forms — placeholder text + example
// values per driver. NOT used at runtime; pure UX surface served from
// GET /api/v1/system/driver-defaults so the cluster + key forms can
// render driver-aware placeholders and help text instead of empty
// inputs the operator has to remember the format of.
//
// v1.3.0b: introduced so the Add Cluster / Edit Cluster / Add Key
// pages stop being "guess the port" puzzles. Keep entries short — the
// frontend renders these inline below inputs.
type EndpointDefaults struct {
	// Driver matches Driver.Name() / the registry key (e.g. "garage-v1").
	Driver string `json:"driver"`
	// DisplayName is a human label for the driver ("Garage v1", "AWS S3").
	DisplayName string `json:"displayName"`
	// AdminURL is an example admin URL placeholder. Empty for drivers
	// with no admin URL concept (AWS S3 has no admin plane).
	AdminURL string `json:"adminUrl"`
	// AdminURLHint is one-sentence help text for the admin URL field.
	AdminURLHint string `json:"adminUrlHint"`
	// S3Endpoint is an example S3 endpoint placeholder.
	S3Endpoint string `json:"s3Endpoint"`
	// S3EndpointHint is one-sentence help text for the S3 endpoint field.
	S3EndpointHint string `json:"s3EndpointHint"`
	// RegionLabel is the typical region string for this driver
	// ("garage", "us-east-1").
	RegionLabel string `json:"regionLabel"`
	// SecretURL is an optional link to docs explaining where to find
	// credentials (e.g. AWS IAM console). Empty when no such page
	// exists.
	SecretURL string `json:"secretUrl,omitempty"`
}

// defaultsTable is the curated registry of per-driver hints. Keep in
// sync with the registered driver names in internal/drivers/*. New
// drivers SHOULD add an entry here even if all the strings are empty
// so the forms render a known displayName rather than the raw id.
var defaultsTable = []EndpointDefaults{
	{
		Driver:         "garage-v1",
		DisplayName:    "Garage v1",
		AdminURL:       "http://garage-host:3903",
		AdminURLHint:   "Garage admin API, default port 3903.",
		S3Endpoint:     "http://garage-host:3902",
		S3EndpointHint: "Garage S3 API, default port 3902.",
		RegionLabel:    "garage",
	},
	{
		Driver:         "garage",
		DisplayName:    "Garage v2",
		AdminURL:       "http://garage-host:3903",
		AdminURLHint:   "Garage v2 admin API, default port 3903.",
		S3Endpoint:     "http://garage-host:3902",
		S3EndpointHint: "Garage v2 S3 API, default port 3902.",
		RegionLabel:    "garage",
	},
	{
		Driver:         "aws-s3",
		DisplayName:    "AWS S3",
		AdminURL:       "",
		AdminURLHint:   "AWS S3 has no admin URL — leave blank.",
		S3Endpoint:     "https://s3.us-east-1.amazonaws.com",
		S3EndpointHint: "AWS S3 regional endpoint; substitute your region.",
		RegionLabel:    "us-east-1",
		SecretURL:      "https://console.aws.amazon.com/iam/home#/security_credentials",
	},
	{
		Driver:         "minio",
		DisplayName:    "MinIO / OpenMaxIO",
		AdminURL:       "http://minio-host:9001",
		AdminURLHint:   "MinIO console, default port 9001.",
		S3Endpoint:     "http://minio-host:9000",
		S3EndpointHint: "MinIO S3 API, default port 9000.",
		RegionLabel:    "us-east-1",
	},
}

// Defaults returns the curated EndpointDefaults entries for every
// driver type known to the registry. Drivers without an entry in
// defaultsTable get a stub with just the Driver id + DisplayName set
// to the raw id, so the UI still renders a complete list. The order
// matches Registered() (sorted by name) for stable rendering.
func Defaults() []EndpointDefaults {
	registered := Registered()
	byName := make(map[string]EndpointDefaults, len(defaultsTable))
	for _, d := range defaultsTable {
		byName[d.Driver] = d
	}

	out := make([]EndpointDefaults, 0, len(registered))
	for _, name := range registered {
		if d, ok := byName[name]; ok {
			out = append(out, d)
			continue
		}
		// Fallback for any driver registered without a curated entry —
		// keeps the API honest (one entry per registered driver) while
		// still surfacing the new driver name to the UI.
		out = append(out, EndpointDefaults{
			Driver:      name,
			DisplayName: name,
		})
	}
	return out
}
