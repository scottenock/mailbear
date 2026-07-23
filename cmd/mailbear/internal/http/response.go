package http

type mailbearResponse struct {
	Message string `json:"message"`
}

func response(message string) mailbearResponse {
	return mailbearResponse{Message: message}
}
