package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/google/generative-ai-go/genai"
	"github.com/gorilla/websocket"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
	"google.golang.org/api/option"

	"Audio-LLM-Contextual-Heygen/audio"
	"Audio-LLM-Contextual-Heygen/embedstore"
	"Audio-LLM-Contextual-Heygen/extract"
)

const (
	googleSearchURL = "https://www.googleapis.com/customsearch/v1"
	geminiAPIURL    = "https://api.gemini.com/v1/embedding"
)

type Result struct {
	Title string `json:"title"`
	Link  string `json:"link"`
	IsTED bool   `json:"isted"`
}

type ChunkData struct {
	Title string
	Link  string
	Text  string
}

var (
	apiKey    string
	cxID      string
	g_Api_Key string
	neo4jURI  string
	neo4jUser string
	neo4jPass string
)

func loadEnvVars() {
	apiKey = os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		fmt.Println("Warning: GOOGLE_API_KEY is not set")
	}

	cxID = os.Getenv("CX_ID")
	if cxID == "" {
		fmt.Println("Warning: CX_ID is not set")
	}

	g_Api_Key = os.Getenv("G_API_KEY")
	if g_Api_Key == "" {
		fmt.Println("Warning: G_API_KEY is not set")
	}

	neo4jURI = os.Getenv("NEO4J_URI")
	neo4jUser = os.Getenv("NEO4J_USER")
	neo4jPass = os.Getenv("NEO4J_PASS")
}

func handleVideoUpload(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tempFile, err := os.CreateTemp("", "video-*.mp4")
	if err != nil {
		http.Error(w, "Error creating temporary file", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tempFile.Name())

	_, err = io.Copy(tempFile, file)
	if err != nil {
		http.Error(w, "Error saving the file", http.StatusInternalServerError)
		return
	}

	extractedText, err := extractTextFromVideo(tempFile.Name())
	if err != nil {
		http.Error(w, "Error extracting text from video", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"extractedText": extractedText})
}

func extractTextFromVideo(videoPath string) (string, error) {
	cmd := exec.Command("ffmpeg", "-i", videoPath, "-vf", "subtitles=subtitles.srt", "-f", "rawvideo", "-")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg command failed: %v", err)
	}
	return string(output), nil
}

func updateGraphDB(query, answer string) error {
	driver, err := neo4j.NewDriver(neo4jURI, neo4j.BasicAuth(neo4jUser, neo4jPass, ""))
	if err != nil {
		return fmt.Errorf("failed to create driver: %w", err)
	}
	defer driver.Close()

	session := driver.NewSession(neo4j.SessionConfig{})
	defer session.Close()

	_, err = session.WriteTransaction(func(tx neo4j.Transaction) (interface{}, error) {
		result, err := tx.Run(
			"MERGE (q:Query {text: $queryText}) "+
				"MERGE (a:Answer {text: $answerText}) "+
				"MERGE (q)-[:HAS_ANSWER]->(a)",
			map[string]interface{}{
				"queryText":  query,
				"answerText": answer,
			})
		if err != nil {
			return nil, err
		}
		return result.Consume()
	})

	return err
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading to WebSocket:", err)
		return
	}
	defer conn.Close()

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			log.Println("Error reading message:", err)
			return
		}

		if messageType == websocket.TextMessage {
			query := string(p)
			response, err := handleUserInteraction(query)
			if err != nil {
				log.Println("Error handling user interaction:", err)
				continue
			}

			audioData, err := audio.ConvertTextToSpeech(response)
			if err != nil {
				log.Println("Error converting text to speech:", err)
				continue
			}

			err = conn.WriteMessage(websocket.BinaryMessage, audioData)
			if err != nil {
				log.Println("Error writing audio data:", err)
				return
			}
		}
	}
}

func handleUserInteraction(query string) (string, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(g_Api_Key))
	if err != nil {
		return "", err
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-1.5-flash")
	response, err := queryLLM(ctx, model, client, query, 300)
	if err != nil {
		return "", err
	}

	return formatResponse(response), nil
}

var totalChunks = 0

func GoogleSearch(query string, maxResults int, resultsCh chan<- embedstore.Result, linkSet map[string]struct{}) {
	u, err := url.Parse(googleSearchURL)
	if err != nil {
		log.Fatal(err)
	}
	q := u.Query()
	q.Set("q", url.QueryEscape(query))
	q.Set("key", apiKey)
	q.Set("cx", cxID)
	u.RawQuery = q.Encode()

	// Log the full URL for debugging purposes
	log.Println("Google Search URL:", u.String())

	resp, err := http.Get(u.String())
	if err != nil {
		log.Println("Error making request to Google Search API:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("Non-OK HTTP status:", resp.StatusCode, resp)
		return
	}

	var response struct {
		Items []embedstore.Result `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		log.Println("Error decoding response:", err)
		return
	}

	for i, result := range response.Items {
		if i >= maxResults {
			break
		}
		// if _, exists := linkSet[result.Link]; !exists {
		// linkSet[result.Link] = struct{}{}
		resultsCh <- result
		// }
	}
}

func TedSearch(query string, maxResults int, resultsCh chan<- embedstore.Result, linkSet map[string]struct{}) {
	u, err := url.Parse(googleSearchURL)
	if err != nil {
		log.Fatal(err)
	}
	q := u.Query()
	q.Set("q", fmt.Sprintf("TED Talk %s", url.QueryEscape(query)))
	q.Set("key", apiKey)
	q.Set("cx", cxID)
	u.RawQuery = q.Encode()

	// Log the full URL for debugging purposes
	fmt.Println("TED Search URL:", u.String())

	resp, err := http.Get(u.String())
	if err != nil {
		fmt.Println("Error making request to Google Search API - TED:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Println("Non-OK HTTP status:", resp.StatusCode)
		return
	}

	var response struct {
		Items []embedstore.Result `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		fmt.Println("Error decoding response:", err)
		return
	}

	for i, result := range response.Items {
		if i >= maxResults {
			break
		}
		// if _, exists := linkSet[result.Link]; !exists {
		// linkSet[result.Link] = struct{}{}
		fmt.Println("Ted result : ", result)
		result.IsTED = true
		resultsCh <- result
		// }
	}
}

var talkMap = make(map[string]extract.TEDTalk)

func LoadTEDTalks(filename string) ([]extract.TEDTalk, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	byteValue, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	var tedTalks []extract.TEDTalk
	if err := json.Unmarshal(byteValue, &tedTalks); err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %v", err)
	}

	for _, talk := range tedTalks {
		key := fmt.Sprintf("%s|%s", talk.Title, talk.Speaker)
		talkMap[key] = talk
	}
	return tedTalks, nil
}

type LLMRequest struct {
	Query     string `json:"query"`
	Context   string `json:"context"`
	MaxTokens int    `json:"max_tokens"`
}

type LLMResponse struct {
	Answer string `json:"answer"`
}

func queryLLMTest(ctx context.Context, model *genai.GenerativeModel, client *genai.Client, llmQuery string, maxTokens int) (s string) {
	resp, err := model.GenerateContent(ctx, genai.Text(llmQuery))
	if err != nil {
		log.Fatal(err)
	}

	return printResponseTest(resp)
}

func printResponseTest(resp *genai.GenerateContentResponse) (s string) {
	x := ""
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				x += fmt.Sprintln(part)
			}
		}
	}
	x += fmt.Sprintln("---")
	fmt.Println(x)
	return x
}

func queryLLM(ctx context.Context, model *genai.GenerativeModel, client *genai.Client, llmQuery string, maxTokens int) (*genai.GenerateContentResponse, error) {
	// Use the provided llmQuery
	resp, err := model.GenerateContent(ctx, genai.Text(llmQuery))
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func formatResponse(resp *genai.GenerateContentResponse) string {
	var sb strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				x := fmt.Sprintln(part)
				fmt.Println("CLIENT OP : ", x)
				sb.WriteString(x)
				sb.WriteString("\n")
			}
		}
	}
	sb.WriteString("---\n")
	return sb.String()
}

func main() {

	loadEnvVars()

	// query := flag.String("query", "", "Search query")
	// flag.Parse()

	// if *query == "" {
	// 	log.Fatal("Search query must be provided")
	// }

	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if query == "" {
			http.Error(w, "Search query must be provided", http.StatusBadRequest)
			return
		}

		// Handling spaces in the query parameter
		fmt.Println("Before ", query)
		query = strings.ReplaceAll(query, "+", " ")
		fmt.Println("After ", query)

		ctx := context.Background()
		client, err := genai.NewClient(ctx, option.WithAPIKey(g_Api_Key))
		if err != nil {
			log.Fatal(err)
		}
		defer client.Close()

		fmt.Println("loading json")
		tedTalks, _ := LoadTEDTalks("new_op.json")
		dimension := 768

		fmt.Printf("Embedding dimensions: %d\n", dimension)

		// Setup of Qdrant collection with the dimension
		embedstore.SetupQdrantCollection(dimension)

		// Channel to receive search results and a wait group till evry gets bback
		resultsCh := make(chan embedstore.Result)
		var wg sync.WaitGroup

		linkSet := make(map[string]struct{})

		wg.Add(2)
		go func() {
			defer wg.Done()
			GoogleSearch(query, 8, resultsCh, linkSet)
		}()
		go func() {
			defer wg.Done()
			TedSearch(query, 3, resultsCh, linkSet)
		}()

		// end recv
		go func() {
			wg.Wait()
			close(resultsCh)
		}()

		// Process each result from the search results channel
		var processWg sync.WaitGroup
		for result := range resultsCh {
			fmt.Println("Title:", result.Title)
			fmt.Println("Link:", result.Link)
			processWg.Add(1)
			go func(result embedstore.Result) {
				// Scrape the content from the search result link
				defer processWg.Done()
				// content, _ := scrape(result, tedTalks)
				content, _ := extract.Scrape(result, tedTalks)
				// fmt.Println("CONTENTTTT : ", document.PageContent)
				// content := document.PageContent
				if content != "" {
					// Generating an embedding for the scraped content
					embedstore.GetGeminiEmbedding(ctx, client, content, "embedding-001", result.Title, result, false)
					// print("Embeddingsssqherr LEN ", len(embedding))
					if err != nil {
						log.Println("Error getting embedding:", err)
						return
					}
				}
				return
			}(result)

		}
		processWg.Wait()
		fmt.Printf("Total embeddings : %d\n", totalChunks)
		fmt.Println()

		// Generate an embedding for the search query

		queryEmbedding := embedstore.GetGeminiEmbedding(ctx, client, query, "embedding-001", "abc", embedstore.Result{}, true)
		if err != nil {
			log.Fatalf("Error generating query embedding: %v", err)
		}
		print("QEMED : ", queryEmbedding)

		// Search for similar embeddings in Qdrant using the query embedding
		limit := 10
		var scoreThreshold float32 = 0.6
		chunkIDs, err := embedstore.SearchQdrant(queryEmbedding, limit, scoreThreshold)
		if err != nil {
			log.Fatalf("Error searching Qdrant: %v", err)
		}

		// Retrieve the content chunks corresponding to the found chunk IDs
		chunks, err := embedstore.GetChunks(chunkIDs)
		if err != nil {
			log.Fatalf("Error retrieving chunks: %v", err)
		}

		// chunks := chunkToDocuments(c, 8192)

		fmt.Println("CHONSKKS ", "$$$", chunks)

		// fmt.Print("CHUNKKSKSKS", chunks)
		// Combininng the retrieved chunks into a single context string
		instruction := `You are a helpful AI assistant that helps users answer queries using the provided context. If you cant frame an answer from the context given, copy paste directly from context rather than making up an answer. Please provide a detailed answer to the query below only using the context provided. Include in-text citations like this [1] for each fact or statement at the end of the sentence. At the end of your response, list all sources in a citation section with the format: [citation number] Name - URL.`

		context := ""
		for _, chunk := range chunks {
			if chunk.Text == query {
				continue
			}
			context += "Title of the website where the following paragraph was obtained from -> " + chunk.Title + ". Link of the website -> " + chunk.Link + " . Paragraph -> " + chunk.Text + " . End of that paragraph.\n Starting new paragraph :  \n"
		}
		llmquery := "INSTRUCTION : " + instruction + ". QUERY : " + query + ". CONTEXT : " + context + "."
		model := client.GenerativeModel("gemini-1.5-flash")
		fmt.Print("LLM QUERY FINAL : ", llmquery)

		s := queryLLMTest(ctx, model, client, llmquery, 300)
		// s := invokeLLMChain(ctx, model, client, chunks, query)
		updateGraphDB(query, s)
		w.Write([]byte(s))

	})

	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
