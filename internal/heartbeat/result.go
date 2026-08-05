package heartbeat

type Status string

const (
	Healthy                  Status = "healthy"
	Timeout                  Status = "timeout"
	DNSFailure               Status = "dns_failure"
	TLSFailure               Status = "tls_failure"
	CredentialRejected       Status = "credential_rejected"
	DatabasePermissionDenied Status = "database_permission_denied"
	APIAuthenticationFailed  Status = "api_authentication_failed"
	TemporaryUpstreamFailure Status = "temporary_upstream_failure"
	ProjectPaused            Status = "project_paused"
	UnexpectedResponse       Status = "unexpected_response"
	MissingInput             Status = "missing_input"
	NoMatchingRow            Status = "no_matching_row"
	ResponseTooLarge         Status = "response_too_large"
	InternalError            Status = "internal_error"
)

type Error struct {
	Code    Status `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	Name       string `json:"name"`
	Status     Status `json:"status"`
	HTTPStatus *int   `json:"http_status"`
	LatencyMS  *int64 `json:"latency_ms"`
	Attempts   int    `json:"attempts"`
	Error      *Error `json:"error"`
}

func ExitCode(results []Result) int {
	for _, result := range results {
		if result.Status != Healthy {
			return 1
		}
	}
	return 0
}
