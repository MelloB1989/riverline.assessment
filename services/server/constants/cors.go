package constants

func GetAllowedOrigins() []string {
	origins := []string{
		"https://riverline.mellob.in",
	}

	if AppCfg.Get().Environment == "DEV" {
		origins = append(origins,
			"http://localhost:3000",
		)
	}

	return origins
}
