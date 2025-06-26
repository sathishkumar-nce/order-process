// handlers/sku_extractor.go
package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tealeg/xlsx/v3"
)

type SKUData struct {
	SKU       string
	Thickness string
	Dimension string
	Weight    float64
}

type ProcessResult struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	OutputURL string `json:"output_url,omitempty"`
	FileName  string `json:"file_name,omitempty"`
}

func ProcessSKUHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form
	err := r.ParseMultipartForm(10 << 20) // 10 MB limit
	if err != nil {
		writeJSONError(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get text content from form
	textContent := r.FormValue("textContent")
	if strings.TrimSpace(textContent) == "" {
		writeJSONError(w, "Text content is required", http.StatusBadRequest)
		return
	}

	// Get mapping file
	mappingFile, mappingHeader, err := r.FormFile("mapping")
	if err != nil {
		writeJSONError(w, "Mapping file is required: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer mappingFile.Close()

	// Save mapping file
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	uploadDir := "./uploads"
	outputDir := "./outputs"

	mappingPath := filepath.Join(uploadDir, timestamp+"_"+mappingHeader.Filename)

	if err := saveFile(mappingFile, mappingPath); err != nil {
		writeJSONError(w, "Failed to save mapping file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Process content
	outputFile := filepath.Join(outputDir, timestamp+"_sku_report.csv")
	fileName := timestamp + "_sku_report.csv"

	err = processSKUContent(textContent, mappingPath, outputFile)

	// Clean up uploaded file
	os.Remove(mappingPath)

	if err != nil {
		writeJSONError(w, "Failed to process content: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return success response
	writeJSONSuccess(w, "SKU extraction completed successfully!", "/outputs/"+fileName, fileName)
}

func processSKUContent(textContent, mappingPath, outputPath string) error {
	// Extract SKUs from text content
	skus := extractSKUs(textContent)
	if len(skus) == 0 {
		return fmt.Errorf("no SKUs found in text content")
	}

	// Read Excel mapping file
	skuMap, err := readExcelMapping(mappingPath)
	if err != nil {
		return fmt.Errorf("error reading Excel mapping: %v", err)
	}

	// Create output CSV
	err = createOutputCSV(skus, skuMap, outputPath)
	if err != nil {
		return fmt.Errorf("error creating output CSV: %v", err)
	}

	return nil
}

// extractSKUs extracts all SKU values from the input text
func extractSKUs(text string) []string {
	// Regular expression to match "SKU: [value]"
	re := regexp.MustCompile(`SKU:\s*([^\s\n\r]+)`)
	matches := re.FindAllStringSubmatch(text, -1)

	var skus []string
	seen := make(map[string]bool) // To avoid duplicates

	for _, match := range matches {
		if len(match) > 1 {
			sku := strings.TrimSpace(match[1])
			if !seen[sku] {
				skus = append(skus, sku)
				seen[sku] = true
			}
		}
	}

	return skus
}

// readExcelMapping reads the Excel file and creates a map of SKU to data
func readExcelMapping(filename string) (map[string]SKUData, error) {
	skuMap := make(map[string]SKUData)

	// Open Excel file
	file, err := xlsx.OpenFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open Excel file: %v", err)
	}

	// Get the first sheet
	if len(file.Sheets) == 0 {
		return nil, fmt.Errorf("no sheets found in Excel file")
	}

	sheet := file.Sheets[0]
	maxRow := sheet.MaxRow
	maxCol := sheet.MaxCol

	if maxCol < 4 {
		return nil, fmt.Errorf("Excel file must have at least 4 columns (SKU, Thickness, Dimension, Weight)")
	}

	// Skip header row (start from row 1) and process data rows
	for rowIdx := 1; rowIdx < maxRow; rowIdx++ {
		// Get cell values safely
		skuCell, err := sheet.Cell(rowIdx, 0)
		if err != nil {
			continue
		}
		sku := strings.TrimSpace(skuCell.String())

		// Skip empty rows
		if sku == "" {
			continue
		}

		thickness := ""
		if thicknessCell, err := sheet.Cell(rowIdx, 1); err == nil {
			thickness = strings.TrimSpace(thicknessCell.String())
		}

		dimension := ""
		if dimensionCell, err := sheet.Cell(rowIdx, 2); err == nil {
			dimension = strings.TrimSpace(dimensionCell.String())
		}

		weight := 0.0
		if weightCell, err := sheet.Cell(rowIdx, 3); err == nil {
			weightStr := strings.TrimSpace(weightCell.String())
			if weightStr != "" {
				if parsedWeight, parseErr := strconv.ParseFloat(weightStr, 64); parseErr == nil {
					weight = parsedWeight
				}
			}
		}

		// Store in map
		skuMap[sku] = SKUData{
			SKU:       sku,
			Thickness: thickness,
			Dimension: dimension,
			Weight:    weight,
		}
	}

	return skuMap, nil
}

// createOutputCSV creates the output CSV file with mapped data
func createOutputCSV(skus []string, skuMap map[string]SKUData, filename string) error {
	// Create output file
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer file.Close()

	// Create CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"SKU", "Thickness", "Dimension", "Weight (kg)", "Status"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %v", err)
	}

	// Count stats
	foundCount := 0
	notFoundCount := 0

	// Write data rows in the same order as input
	for _, sku := range skus {
		if data, exists := skuMap[sku]; exists {
			// SKU found in mapping
			row := []string{
				data.SKU,
				data.Thickness,
				data.Dimension,
				fmt.Sprintf("%.3f", data.Weight),
				"Found",
			}
			if err := writer.Write(row); err != nil {
				return fmt.Errorf("failed to write row: %v", err)
			}
			foundCount++
		} else {
			// SKU not found in mapping
			row := []string{
				sku,
				"",
				"",
				"",
				"Not Found",
			}
			if err := writer.Write(row); err != nil {
				return fmt.Errorf("failed to write row: %v", err)
			}
			notFoundCount++
		}
	}

	// Write summary row
	summaryRow := []string{
		fmt.Sprintf("SUMMARY: %d Total SKUs", len(skus)),
		fmt.Sprintf("%d Found", foundCount),
		fmt.Sprintf("%d Not Found", notFoundCount),
		fmt.Sprintf("%.1f%% Match Rate", float64(foundCount)/float64(len(skus))*100),
		"Summary",
	}
	if err := writer.Write(summaryRow); err != nil {
		return fmt.Errorf("failed to write summary: %v", err)
	}

	return nil
}

// Helper functions for JSON responses
func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	result := ProcessResult{
		Success: false,
		Message: message,
	}

	json.NewEncoder(w).Encode(result)
}

func writeJSONSuccess(w http.ResponseWriter, message, outputURL, fileName string) {
	w.Header().Set("Content-Type", "application/json")

	result := ProcessResult{
		Success:   true,
		Message:   message,
		OutputURL: outputURL,
		FileName:  fileName,
	}

	json.NewEncoder(w).Encode(result)
}
