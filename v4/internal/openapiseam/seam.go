/* Package openapiseam is the door the application ends the openapi registry's wiring phase through; Token cannot be spelled outside v4, so the door is not callable from userland. */
package openapiseam

type Token struct{}

type ServingMarker interface {
    MarkServing(token Token)
}
