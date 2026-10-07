package utils

import "strings"

// foldAccents quita las tildes del español. La ñ se conserva: es otra letra.
var foldAccents = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u",
)

// FoldText normaliza un texto para comparar o buscar: minúsculas, sin tildes
// y con los espacios colapsados. "  Recepción  B " -> "recepcion b".
func FoldText(s string) string {
	return foldAccents.Replace(strings.Join(strings.Fields(strings.ToLower(s)), " "))
}
