package contract

import (
    "reflect"
)

/* HiddenType and RiderType answer unnamed types built from what String() leaves out: an unexported field and an unexported method, whose package only reflection names. The twin package answers types of the same String(). */
func HiddenType() reflect.Type {
    return reflect.TypeOf(struct{ hidden int }{})
}

func RiderType() reflect.Type {
    return reflect.TypeOf((*interface{ ride() })(nil)).Elem()
}
