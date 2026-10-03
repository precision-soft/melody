package openapi

type createProductRequest struct {
    Name  string  `json:"name" validate:"notBlank,min=2"`
    Email string  `json:"email" validate:"email"`
    Price float64 `json:"price"`
}

type productResponse struct {
    Id   string `json:"id"`
    Name string `json:"name"`
}
