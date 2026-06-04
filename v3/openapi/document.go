package openapi

type Document struct {
    OpenApi    string              `json:"openapi"`
    Info       Info                `json:"info"`
    Paths      map[string]PathItem `json:"paths"`
    Components *Components         `json:"components,omitempty"`
}

type Info struct {
    Title       string `json:"title"`
    Version     string `json:"version"`
    Description string `json:"description,omitempty"`
}

type Components struct {
    Schemas map[string]*Schema `json:"schemas,omitempty"`
}

type PathItem struct {
    Get    *Operation `json:"get,omitempty"`
    Post   *Operation `json:"post,omitempty"`
    Put    *Operation `json:"put,omitempty"`
    Patch  *Operation `json:"patch,omitempty"`
    Delete *Operation `json:"delete,omitempty"`
}

type Operation struct {
    OperationId string                    `json:"operationId,omitempty"`
    Summary     string                    `json:"summary,omitempty"`
    Description string                    `json:"description,omitempty"`
    Tags        []string                  `json:"tags,omitempty"`
    Parameters  []Parameter               `json:"parameters,omitempty"`
    RequestBody *RequestBody              `json:"requestBody,omitempty"`
    Responses   map[string]ResponseObject `json:"responses"`
}

type Parameter struct {
    Name     string  `json:"name"`
    In       string  `json:"in"`
    Required bool    `json:"required"`
    Schema   *Schema `json:"schema,omitempty"`
}

type RequestBody struct {
    Required bool                 `json:"required,omitempty"`
    Content  map[string]MediaType `json:"content"`
}

type ResponseObject struct {
    Description string               `json:"description"`
    Content     map[string]MediaType `json:"content,omitempty"`
}

type MediaType struct {
    Schema *Schema `json:"schema,omitempty"`
}

type Schema struct {
    Type                 string             `json:"type,omitempty"`
    Format               string             `json:"format,omitempty"`
    Properties           map[string]*Schema `json:"properties,omitempty"`
    Required             []string           `json:"required,omitempty"`
    Items                *Schema            `json:"items,omitempty"`
    AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
    MinLength            *int               `json:"minLength,omitempty"`
    MaxLength            *int               `json:"maxLength,omitempty"`
    Minimum              *float64           `json:"minimum,omitempty"`
    Pattern              string             `json:"pattern,omitempty"`
}
