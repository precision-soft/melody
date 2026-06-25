package openapi

type Document struct {
    OpenApi      string                `json:"openapi"`
    Info         Info                  `json:"info"`
    Servers      []Server              `json:"servers,omitempty"`
    Paths        map[string]PathItem   `json:"paths"`
    Components   *Components           `json:"components,omitempty"`
    Security     []map[string][]string `json:"security,omitempty"`
    Tags         []Tag                 `json:"tags,omitempty"`
    ExternalDocs *ExternalDocs         `json:"externalDocs,omitempty"`
}

type Info struct {
    Title       string `json:"title"`
    Version     string `json:"version"`
    Description string `json:"description,omitempty"`
}

type Server struct {
    Url         string `json:"url"`
    Description string `json:"description,omitempty"`
}

type Tag struct {
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
}

type ExternalDocs struct {
    Url         string `json:"url"`
    Description string `json:"description,omitempty"`
}

type Components struct {
    Schemas map[string]*Schema `json:"schemas,omitempty"`
}

type PathItem struct {
    Get     *Operation `json:"get,omitempty"`
    Post    *Operation `json:"post,omitempty"`
    Put     *Operation `json:"put,omitempty"`
    Patch   *Operation `json:"patch,omitempty"`
    Delete  *Operation `json:"delete,omitempty"`
    Options *Operation `json:"options,omitempty"`
    Head    *Operation `json:"head,omitempty"`
    Trace   *Operation `json:"trace,omitempty"`
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
    Ref                  string             `json:"$ref,omitempty"`
    AllOf                []*Schema          `json:"allOf,omitempty"`
    Type                 string             `json:"type,omitempty"`
    Format               string             `json:"format,omitempty"`
    Description          string             `json:"description,omitempty"`
    Nullable             bool               `json:"nullable,omitempty"`
    Properties           map[string]*Schema `json:"properties,omitempty"`
    Required             []string           `json:"required,omitempty"`
    Items                *Schema            `json:"items,omitempty"`
    AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
    MinLength            *int               `json:"minLength,omitempty"`
    MaxLength            *int               `json:"maxLength,omitempty"`
    MinItems             *int               `json:"minItems,omitempty"`
    MaxItems             *int               `json:"maxItems,omitempty"`
    MinProperties        *int               `json:"minProperties,omitempty"`
    MaxProperties        *int               `json:"maxProperties,omitempty"`
    Minimum              *float64           `json:"minimum,omitempty"`
    Maximum              *float64           `json:"maximum,omitempty"`
    ExclusiveMinimum     *bool              `json:"exclusiveMinimum,omitempty"`
    ExclusiveMaximum     *bool              `json:"exclusiveMaximum,omitempty"`
    Pattern              string             `json:"pattern,omitempty"`
    Enum                 *[]any             `json:"enum,omitempty"`
}
