/* Package configseam is the door the application ends the configuration's wiring phase through; Token cannot be spelled outside v4, so the door is not callable from userland. */
package configseam

type Token struct{}

type ServingMarker interface {
    MarkServing(token Token)
}
