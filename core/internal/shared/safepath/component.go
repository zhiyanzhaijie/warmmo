package safepath

import (
	"crypto/sha256"
	"fmt"
)

func Component(value string) string {
	for _, character := range value {
		if !isSafeCharacter(character) {
			return digest(value)
		}
	}
	if value == "" {
		return digest(value)
	}
	return value
}

func isSafeCharacter(character rune) bool {
	return (character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9') ||
		character == '-' || character == '_'
}

func digest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", hash[:12])
}
