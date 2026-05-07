package httpx

type ErrorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func Error(status string, message string) ErrorResponse {
	return ErrorResponse{
		Status:  status,
		Message: message,
	}
}
