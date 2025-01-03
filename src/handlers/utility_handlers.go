package handlers

import (
	"encoding/json"
	"net/http"
)

func AssociatedDomains(w http.ResponseWriter, r *http.Request) {
	associatedDomains := map[string]interface{}{
		"webcredentials": map[string]interface{}{
			"apps": []string{"9G8Z84JPGV.com.lastweekend.app"},
		},
	}
	responseBytes, err := json.Marshal(associatedDomains)
	if err != nil {
		http.Error(w, "Failed to marshal associated domains", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}
