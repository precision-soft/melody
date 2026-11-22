/* Package routeseam is the door the application installs its boot collision recorder on the route registry through; Token cannot be spelled outside v4, so the door is not callable from userland. */
package routeseam

type Token struct{}

type BootCollisionRecorderInstaller interface {
    InstallBootCollisionRecorder(token Token, recorder func(kind string, name string))
}
