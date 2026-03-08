package url

import "strings"

func NewGenerator() *Generator {
    return &Generator{}
}

type Generator struct{}

func (instance *Generator) Generate(pattern string, params map[string]string) string {
    urlString := pattern

    for key, value := range params {
        normalizedKey := strings.TrimSpace(key)
        if "" == normalizedKey {
            continue
        }
        token := ":" + normalizedKey

        urlString = strings.ReplaceAll(urlString, token, value)
    }

    return urlString
}
