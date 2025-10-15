module github.com/BYTE-6D65/netadapters

go 1.25.2

require (
	github.com/BYTE-6D65/pipeline v0.0.0-20251011174147-291b3c618a12
	github.com/google/uuid v1.6.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20250910080747-cc2cfa0554c3 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/sys v0.36.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
)

// Use local pipeline for development with telemetry
replace github.com/BYTE-6D65/pipeline => /Users/liam/Projects/02_scratchpad/pipeline
