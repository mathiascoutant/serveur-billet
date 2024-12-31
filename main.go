package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	apiKey    = "181ab7df159cabc79844833c13f911bb"
	username  = "mathiascoutant@icloud.com"
	password  = "mathias"
	eventID   = "1220040"
	wsURL     = "ws://[2a01:cb18:c03:f701:99:66cc:d520:c22]:4455"
	wsPass    = "mathias"
	sceneName = "Debut"
)

var (
	roles              = []string{"Réalisateur", "Acteur", "Producteur"}
	films              = []string{
		"Amour", "La Haine", "Intouchables", "Le Fabuleux Destin d'Amélie Poulain",
		"Les Choristes", "La La Land", "Les Misérables", "Un prophète",
		"Le Grand Bleu", "Caché", "Léon", "La Vie en Rose",
		"Les 400 Coups", "Delicatessen", "Au revoir les enfants", "La Gloire de mon père",
		"Les Parapluies de Cherbourg", "L'Atelier", "La Femme Nikita", "Tanguy",
		"Le Dîner de cons", "Les Visiteurs", "Bienvenue chez les Ch'tis", "Intouchables",
		"Le Prénom", "L'Arnacoeur", "Les Petits Mouchoirs", "La Famille Bélier",
		"Les Biches", "Lola", "La Jetée", "Les Diaboliques",
		"Un long dimanche de fiançailles", "L'Enfant", "La Cité des enfants perdus", "Les Aventures de Rabbi Jacob",
		"Le Petit Nicolas", "L'Exercice de l'État", "La Délicatesse", "Les Beaux Gosses",
		"Le Sens de la fête", "La Promesse", "L'Ascension", "Les Tuche",
		"Les Frères Sisters", "La Môme", "L'Intouchable", "Le Voyage de Fanny",
		"Les Enfoirés", "La Ch'tite Famille", "L'Ordre et la Morale", "Le Gendarme de Saint-Tropez",
		"Les Trois Frères", "La Guerre des boutons", "L'Enfer", "Le Petit Prince",
	}
	scannedParticipants []SimpleResponse
	participantChoices  = make(map[string]SimpleResponse)
	conn               *websocket.Conn
)

// Structures pour les réponses JSON
type Owner struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type ControlStatus struct {
	Status   string `json:"status"`
	ScanDate string `json:"scan_date"`
}

type Participant struct {
	Owner         Owner         `json:"owner"`
	ControlStatus ControlStatus `json:"control_status"`
}

type ParticipantsResponse struct {
	Participants []Participant `json:"participants"`
	EntryCount   string        `json:"entry_count"`
}

type SimpleResponse struct {
	Role      string `json:"role"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Film      string `json:"film"`
	ScanDate  string `json:"scan_date"`
}

func main() {
	// Initialisation du générateur de nombres aléatoires
	rand.Seed(time.Now().UnixNano())

	// Connexion au WebSocket OBS
	var err error
	conn, _, err = websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": []string{"Bearer " + wsPass}})
	if err != nil {
		log.Fatalf("Erreur de connexion à OBS WebSocket : %v", err)
	}
	defer conn.Close()

	// Obtenir le jeton d'accès
	accessToken, err := getAccessToken()
	if err != nil {
		log.Fatalf("Erreur lors de l'obtention du jeton d'accès : %v", err)
	}

	// Mettre à jour les participants scannés en boucle
	go func() {
		for {
			updateScannedParticipants(accessToken)
			time.Sleep(5 * time.Second)
		}
	}()

	// Démarrer le serveur HTTP
	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/scan", func(w http.ResponseWriter, r *http.Request) {
		handleCORS(w, r)
		if r.Method == http.MethodGet {
			handleScanRequest(w)
		}
	})
	http.HandleFunc("/scan_count", func(w http.ResponseWriter, r *http.Request) {
		handleCORS(w, r)
		if r.Method == http.MethodGet {
			handleScanCount(w, accessToken)
		}
	})

	fmt.Println("Serveur démarré sur http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Gestionnaire pour afficher l'index
func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

// Fonction pour ajouter des en-têtes CORS
func handleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
	}
}

// Gérer les requêtes pour les participants scannés
func handleScanRequest(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	if len(scannedParticipants) == 0 {
		json.NewEncoder(w).Encode([]SimpleResponse{})
		return
	}
	// Limiter à 3 participants maximum
	limit := len(scannedParticipants)
	if limit > 3 {
		limit = 3
	}
	json.NewEncoder(w).Encode(scannedParticipants[:limit])
}

// Gérer les requêtes pour le comptage des scans
func handleScanCount(w http.ResponseWriter, accessToken string) {
	participants, err := getParticipants(accessToken)
	if err != nil {
		http.Error(w, "Erreur lors de l'obtention des participants", http.StatusInternalServerError)
		return
	}

	// Compter les participants
	total := len(participants.Participants)
	scanned := 0
	for _, participant := range participants.Participants {
		if participant.ControlStatus.Status == "1" {
			scanned++
		}
	}

	// Envoyer la réponse
	response := map[string]int{
		"scanned_count": scanned,
		"total_count":   total,
	}
	json.NewEncoder(w).Encode(response)
}

// Mettre à jour les participants scannés
func updateScannedParticipants(accessToken string) {
	participants, err := getParticipants(accessToken)
	if err != nil {
		log.Printf("Erreur lors de l'obtention des participants : %v", err)
		return
	}

	scannedParticipants = nil
	for _, participant := range participants.Participants {
		if participant.ControlStatus.Status == "1" {
			key := participant.Owner.FirstName + " " + participant.Owner.LastName
			if choice, exists := participantChoices[key]; exists {
				scannedParticipants = append(scannedParticipants, choice)
			} else {
				role := roles[rand.Intn(len(roles))]
				film := films[rand.Intn(len(films))]
				choice := SimpleResponse{
					Role:      role,
					FirstName: participant.Owner.FirstName,
					LastName:  participant.Owner.LastName,
					Film:      film,
					ScanDate:  participant.ControlStatus.ScanDate,
				}
				participantChoices[key] = choice
				scannedParticipants = append(scannedParticipants, choice)
			}
		}
	}

	sort.Slice(scannedParticipants, func(i, j int) bool {
		t1, _ := time.Parse("2006-01-02 15:04:05", scannedParticipants[i].ScanDate)
		t2, _ := time.Parse("2006-01-02 15:04:05", scannedParticipants[j].ScanDate)
		return t1.After(t2)
	})
}

// Obtenir le jeton d'accès
func getAccessToken() (string, error) {
	data := url.Values{
		"username": {username},
		"password": {password},
		"api_key":  {apiKey},
	}

	req, err := http.NewRequest("POST", "https://api.weezevent.com/auth/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"accessToken"`
		Error       string `json:"error"`
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("API error: %s", result.Error)
	}
	return result.AccessToken, nil
}

// Obtenir les participants depuis l'API Weezevent
func getParticipants(accessToken string) (ParticipantsResponse, error) {
	url := fmt.Sprintf("https://api.weezevent.com/participant/list?api_key=%s&access_token=%s&id_event[]=%s&full=1", apiKey, accessToken, eventID)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return ParticipantsResponse{}, err
	}
	defer resp.Body.Close()

	var response ParticipantsResponse
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return ParticipantsResponse{}, err
	}
	return response, nil
}
