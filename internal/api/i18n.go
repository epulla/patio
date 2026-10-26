package api

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
)

func notificationText(db *sql.DB, text string) string {
	var stored string
	var language string
	if err := db.QueryRow(`SELECT value FROM app_setting WHERE key='language'`).Scan(&stored); err != nil || json.Unmarshal([]byte(stored), &language) != nil || language == "" {
		language = "system"
	}
	if language == "system" {
		// burnedout: assumed system language from LC_ALL/LC_MESSAGES/LANG
		for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			if value := os.Getenv(key); value != "" {
				if strings.HasPrefix(strings.ToLower(value), "es") {
					language = "es"
				} else {
					language = "en"
				}
				break
			}
		}
		if language == "system" {
			language = "en"
		}
	}
	if language != "es" {
		return text
	}
	translations := map[string]string{
		"Reply finished: ": "Respuesta finalizada: ", "Chat failed: ": "Error en chat: ",
		"Scheduled task finished: ": "Tarea programada finalizada: ", "Scheduled task failed: ": "Error en tarea programada: ",
		"Scheduled task started: ": "Tarea programada iniciada: ", "Reminder: ": "Recordatorio: ",
		"Approval needed: ": "Se requiere aprobación: ", " has a question": " tiene una pregunta",
		"MCP server down: ": "Servidor MCP desconectado: ", "Connector down: ": "Conector desconectado: ",
		"Not installed": "No está instalado", "Not logged in": "No ha iniciado sesión",
		" is available": " está disponible", "Open the release page to download it. You have ": "Abre la página de la versión para descargarla. Tienes ",
	}
	for english, spanish := range translations {
		if strings.HasPrefix(text, english) {
			return spanish + strings.TrimPrefix(text, english)
		}
		if strings.HasSuffix(text, english) {
			return strings.TrimSuffix(text, english) + spanish
		}
		if text == english {
			return spanish
		}
	}
	return text
}
