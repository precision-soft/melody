package config

import (
    melodytranslation "github.com/precision-soft/melody/v3/translation"
)

func (instance *Module) buildTranslation() {
    english := melodytranslation.NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello, {name}!")
    english.Add("messages", "cart.items", "{count, plural, =0 {Your cart is empty} one {# item in your cart} other {# items in your cart}}")

    romanian := melodytranslation.NewMapCatalog("ro")
    romanian.Add("messages", "greeting", "Salut, {name}!")
    romanian.Add("messages", "cart.items", "{count, plural, =0 {Coșul este gol} one {# produs în coș} other {# produse în coș}}")

    instance.translator = melodytranslation.NewManager("en", []string{"en"}, english, romanian)
}
