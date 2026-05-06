package constants

import "os"

func GetAllowedOrigins() []string {
	origins := []string{
		"https://riverline.mellob.in",
	}

	if os.Getenv("ENV") == "DEV" {
		origins = append(origins,
			"http://localhost:3000",
		)
	}

	return origins
}
