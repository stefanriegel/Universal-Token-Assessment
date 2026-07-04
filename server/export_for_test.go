package server

// AggregateFindings is an exported alias for aggregateFindings, used by tests
// in the server_test package.
var AggregateFindings = aggregateFindings

// GCPAuthResult and GCPOAuthCallbackHandler expose the GCP OAuth callback
// internals to tests in the server_test package.
type GCPAuthResult = gcpAuthResult

var GCPOAuthCallbackHandler = gcpOAuthCallbackHandler
