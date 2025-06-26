// handlers/pdf_processor.go
package handlers

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/xuri/excelize/v2"
)

type PDFOrderData struct {
	OrderNumber string
	SKUID       string
	Thickness   string
	Dimension   string
	PageNumber  int
}

type PDFSKUMapping struct {
	SKU       string
	Thickness string
	Dimension string
}

func ProcessPDFHandler(w http.ResponseWriter, r *http.Request) {
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

	// Get uploaded files
	pdfFile, pdfHeader, err := r.FormFile("pdf")
	if err != nil {
		writeJSONError(w, "PDF file is required: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer pdfFile.Close()

	mappingFile, mappingHeader, err := r.FormFile("mapping")
	if err != nil {
		writeJSONError(w, "Mapping file is required: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer mappingFile.Close()

	outputMode := r.FormValue("outputMode")
	if outputMode != "csv" && outputMode != "overlay" {
		outputMode = "csv" // default
	}

	// Save uploaded files
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	uploadDir := "./uploads"
	outputDir := "./outputs"

	pdfPath := filepath.Join(uploadDir, timestamp+"_"+pdfHeader.Filename)
	mappingPath := filepath.Join(uploadDir, timestamp+"_"+mappingHeader.Filename)

	if err := saveFile(pdfFile, pdfPath); err != nil {
		writeJSONError(w, "Failed to save PDF file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := saveFile(mappingFile, mappingPath); err != nil {
		writeJSONError(w, "Failed to save mapping file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Load SKU mapping
	skuMap, err := loadPDFSKUMapping(mappingPath)
	if err != nil {
		os.Remove(pdfPath)
		os.Remove(mappingPath)
		writeJSONError(w, "Failed to load SKU mapping: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Extract text from PDF
	textContent, err := extractTextFromPDF(pdfPath)
	if err != nil {
		os.Remove(pdfPath)
		os.Remove(mappingPath)
		writeJSONError(w, "Failed to extract text from PDF: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Process the extracted text
	orderData, err := processPDFText(textContent, skuMap)
	if err != nil {
		os.Remove(pdfPath)
		os.Remove(mappingPath)
		writeJSONError(w, "Failed to process PDF text: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var outputFile string
	var fileName string

	if outputMode == "overlay" {
		// Create PDF overlay with proper overlaying
		outputFile = filepath.Join(outputDir, timestamp+"_overlaid.pdf")
		fileName = timestamp + "_overlaid.pdf"
		err = createProperPDFOverlay(pdfPath, orderData, outputFile)
	} else {
		// Create CSV
		outputFile = filepath.Join(outputDir, timestamp+"_result.csv")
		fileName = timestamp + "_result.csv"
		err = writePDFToCSV(orderData, outputFile)
	}

	// Clean up uploaded files
	os.Remove(pdfPath)
	os.Remove(mappingPath)

	if err != nil {
		writeJSONError(w, "Failed to create output: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return success response
	writeJSONSuccess(w, "PDF processed successfully!", "/outputs/"+fileName, fileName)
}

func extractTextFromPDF(pdfPath string) (string, error) {
	// Check if pdftotext is available
	if !isPdftotextAvailable() {
		return "", fmt.Errorf("pdftotext is not installed. Please install poppler-utils")
	}

	// Create a temporary file for text output
	tempFile := filepath.Join(os.TempDir(), "temp_output.txt")
	defer os.Remove(tempFile)

	// Run pdftotext command
	cmd := exec.Command("pdftotext", pdfPath, tempFile)
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("pdftotext command failed: %v", err)
	}

	// Read the extracted text
	content, err := os.ReadFile(tempFile)
	if err != nil {
		return "", fmt.Errorf("failed to read extracted text: %v", err)
	}

	return string(content), nil
}

func isPdftotextAvailable() bool {
	_, err := exec.LookPath("pdftotext")
	return err == nil
}

func isPdftkAvailable() bool {
	_, err := exec.LookPath("pdftk")
	if err != nil {
		// Try pdftk-java as well
		_, err = exec.LookPath("pdftk-java")
	}
	return err == nil
}

func processPDFText(text string, skuMap map[string]PDFSKUMapping) ([]PDFOrderData, error) {
	// Split by pages
	pages := strings.Split(text, "\f")
	if len(pages) == 1 {
		pages = splitByOrderPattern(text)
	}

	var allOrders []PDFOrderData

	for pageIdx, pageText := range pages {
		pageNum := pageIdx + 1
		orders := processPageText(pageText, skuMap, pageNum)
		allOrders = append(allOrders, orders...)
	}

	if len(allOrders) == 0 {
		return processTextSimple(text, skuMap)
	}

	return allOrders, nil
}

func splitByOrderPattern(text string) []string {
	orderPattern := regexp.MustCompile(`(?m)Order Number:`)
	indices := orderPattern.FindAllStringIndex(text, -1)

	if len(indices) <= 1 {
		return []string{text}
	}

	var pages []string
	start := 0
	for i := 1; i < len(indices); i++ {
		end := indices[i][0]
		pages = append(pages, text[start:end])
		start = end
	}
	pages = append(pages, text[start:])

	return pages
}

func processPageText(pageText string, skuMap map[string]PDFSKUMapping, pageNum int) []PDFOrderData {
	orderNumberRegex := regexp.MustCompile(`Order Number:\s*(\d{3}-\d{7}-\d{7})`)
	skuRegex := regexp.MustCompile(`MRC-MR-(\d{4})`)

	orderMatches := orderNumberRegex.FindAllStringSubmatch(pageText, -1)
	skuMatches := skuRegex.FindAllStringSubmatch(pageText, -1)

	var orders []PDFOrderData
	minLen := min(len(orderMatches), len(skuMatches))

	for i := 0; i < minLen; i++ {
		if len(orderMatches[i]) > 1 && len(skuMatches[i]) > 1 {
			orderNumber := orderMatches[i][1]
			skuID := "MRC-MR-" + skuMatches[i][1]

			var thickness, dimension string
			if mapping, found := skuMap[skuID]; found {
				thickness = mapping.Thickness
				dimension = mapping.Dimension
			} else {
				thickness = "N/A"
				dimension = "N/A"
			}

			orders = append(orders, PDFOrderData{
				OrderNumber: orderNumber,
				SKUID:       skuID,
				Thickness:   thickness,
				Dimension:   dimension,
				PageNumber:  pageNum,
			})
		}
	}

	return orders
}

func processTextSimple(text string, skuMap map[string]PDFSKUMapping) ([]PDFOrderData, error) {
	orderNumberRegex := regexp.MustCompile(`Order Number:\s*(\d{3}-\d{7}-\d{7})`)
	skuRegex := regexp.MustCompile(`MRC-MR-(\d{4})`)

	orderMatches := orderNumberRegex.FindAllStringSubmatch(text, -1)
	skuMatches := skuRegex.FindAllStringSubmatch(text, -1)

	var orders []PDFOrderData
	minLen := min(len(orderMatches), len(skuMatches))

	for i := 0; i < minLen; i++ {
		if len(orderMatches[i]) > 1 && len(skuMatches[i]) > 1 {
			orderNumber := orderMatches[i][1]
			skuID := "MRC-MR-" + skuMatches[i][1]

			var thickness, dimension string
			if mapping, found := skuMap[skuID]; found {
				thickness = mapping.Thickness
				dimension = mapping.Dimension
			} else {
				thickness = "N/A"
				dimension = "N/A"
			}

			orders = append(orders, PDFOrderData{
				OrderNumber: orderNumber,
				SKUID:       skuID,
				Thickness:   thickness,
				Dimension:   dimension,
				PageNumber:  i + 1,
			})
		}
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("no valid order/SKU pairs found")
	}

	return orders, nil
}

func loadPDFSKUMapping(filename string) (map[string]PDFSKUMapping, error) {
	file, err := excelize.OpenFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open Excel file: %v", err)
	}
	defer file.Close()

	sheetName := file.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("no sheets found in Excel file")
	}

	rows, err := file.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to read rows: %v", err)
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("Excel file must have at least 2 rows (header + data)")
	}

	skuMap := make(map[string]PDFSKUMapping)

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 3 {
			continue
		}

		sku := strings.TrimSpace(row[0])
		thickness := strings.TrimSpace(row[1])
		dimension := strings.TrimSpace(row[2])

		if sku != "" {
			skuMap[sku] = PDFSKUMapping{
				SKU:       sku,
				Thickness: thickness,
				Dimension: dimension,
			}
		}
	}

	return skuMap, nil
}

func writePDFToCSV(orders []PDFOrderData, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	err = writer.Write([]string{"Order Number", "SKU ID", "Thickness", "Dimension", "Page Number"})
	if err != nil {
		return fmt.Errorf("failed to write CSV header: %v", err)
	}

	// Write data
	for _, order := range orders {
		err = writer.Write([]string{
			order.OrderNumber,
			order.SKUID,
			order.Thickness,
			order.Dimension,
			fmt.Sprintf("%d", order.PageNumber),
		})
		if err != nil {
			return fmt.Errorf("failed to write CSV row: %v", err)
		}
	}

	return nil
}

// NEW: Proper PDF overlay implementation based on your working code
func createProperPDFOverlay(inputPDF string, orders []PDFOrderData, outputPDF string) error {
	// Check if pdftk is available for proper overlay
	if !isPdftkAvailable() {
		// Fallback to simple overlay if pdftk is not available
		return createSimplePDFOverlay(orders, outputPDF)
	}

	// Group orders by page
	pageOrders := make(map[int][]PDFOrderData)
	for _, order := range orders {
		pageOrders[order.PageNumber] = append(pageOrders[order.PageNumber], order)
	}

	// Create temporary directory for overlay files
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("temp_overlays_%d", time.Now().Unix()))
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir) // Clean up temp directory

	// Get total number of pages in the input PDF
	totalPages, err := getPDFPageCount(inputPDF)
	if err != nil {
		return fmt.Errorf("failed to get page count: %v", err)
	}

	// Create overlay for each page
	overlayFiles := make([]string, 0)
	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		overlayFile := filepath.Join(tempDir, fmt.Sprintf("overlay_page_%d.pdf", pageNum))

		if pageOrderList, hasOrders := pageOrders[pageNum]; hasOrders {
			// Create overlay with annotation text
			err = createTransparentOverlay(overlayFile, pageOrderList)
			if err != nil {
				return fmt.Errorf("failed to create overlay for page %d: %v", pageNum, err)
			}
		} else {
			// Create empty overlay (transparent page)
			err = createEmptyOverlay(overlayFile)
			if err != nil {
				return fmt.Errorf("failed to create empty overlay for page %d: %v", pageNum, err)
			}
		}
		overlayFiles = append(overlayFiles, overlayFile)
	}

	// Combine all overlays into a single multi-page overlay PDF
	multiOverlayPDF := filepath.Join(tempDir, "multi_overlay.pdf")
	err = combineOverlayPDFs(overlayFiles, multiOverlayPDF)
	if err != nil {
		return fmt.Errorf("failed to combine overlay PDFs: %v", err)
	}

	// Use pdftk to overlay the annotations on the original PDF
	err = overlayWithPdftk(inputPDF, multiOverlayPDF, outputPDF)
	if err != nil {
		return fmt.Errorf("failed to overlay PDFs: %v", err)
	}

	return nil
}

func getPDFPageCount(pdfPath string) (int, error) {
	// Use pdfinfo to get page count (part of poppler-utils)
	cmd := exec.Command("pdfinfo", pdfPath)
	output, err := cmd.Output()
	if err != nil {
		// Fallback: assume 30 pages if pdfinfo is not available
		return 30, nil
	}

	// Parse the output to find page count
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Pages:") {
			var pages int
			fmt.Sscanf(line, "Pages: %d", &pages)
			return pages, nil
		}
	}

	// Fallback
	return 30, nil
}

func createTransparentOverlay(filename string, orders []PDFOrderData) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 14) // Increased font size
	pdf.SetTextColor(0, 0, 0)     // Black color

	// Position annotations in a corner or bottom of the page to avoid content overlap
	startX := 10.0
	startY := 245.0 // Bottom of page

	// Add a semi-transparent background for better readability
	pdf.SetFillColor(255, 255, 255) // White background
	pdf.SetAlpha(0.9, "Normal")     // Semi-transparent

	for i, order := range orders {
		y := startY + float64(i)*15.0 // Spacing between annotations

		// Ensure we don't go off the page
		if y > 280 {
			startX += 120.0 // Move to next column
			startY = 245.0
			y = startY
		}

		// Create annotation text
		text := fmt.Sprintf("Thickness: %s | Dimension: %s", order.Thickness, order.Dimension)

		// Add background rectangle for better readability
		textWidth := pdf.GetStringWidth(text)
		pdf.Rect(startX-2, y-2, textWidth+4, 10, "F")

		// Add text
		pdf.SetXY(startX, y)
		pdf.Cell(textWidth, 8, text)
	}

	return pdf.OutputFileAndClose(filename)
}

func createEmptyOverlay(filename string) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	// Create a completely transparent/empty page
	return pdf.OutputFileAndClose(filename)
}

func combineOverlayPDFs(overlayFiles []string, outputFile string) error {
	// Use pdftk to combine multiple single-page PDFs into one multi-page PDF
	args := append(overlayFiles, "cat", "output", outputFile)
	cmd := exec.Command("pdftk", args...)
	return cmd.Run()
}

func overlayWithPdftk(inputPDF, overlayPDF, outputPDF string) error {
	// Use pdftk to overlay the multi-page overlay onto the original PDF
	cmd := exec.Command("pdftk", inputPDF, "multistamp", overlayPDF, "output", outputPDF)
	err := cmd.Run()
	if err != nil {
		// Try alternative pdftk command
		cmd = exec.Command("pdftk", inputPDF, "stamp", overlayPDF, "output", outputPDF)
		err = cmd.Run()
	}
	return err
}

// Fallback simple overlay for when pdftk is not available
func createSimplePDFOverlay(orders []PDFOrderData, outputPDF string) error {
	// Group orders by page
	pageOrders := make(map[int][]PDFOrderData)
	for _, order := range orders {
		pageOrders[order.PageNumber] = append(pageOrders[order.PageNumber], order)
	}

	// Create a simple overlay PDF (this is the original implementation)
	pdf := gofpdf.New("P", "mm", "A4", "")

	for pageNum, pageOrderList := range pageOrders {
		pdf.AddPage()
		pdf.SetFont("Arial", "B", 12)
		pdf.SetTextColor(0, 0, 0)

		y := 20.0
		pdf.SetXY(10, y)
		pdf.Cell(0, 10, fmt.Sprintf("Page %d Annotations:", pageNum))

		y += 15
		for _, order := range pageOrderList {
			pdf.SetXY(10, y)
			text := fmt.Sprintf("Order: %s | SKU: %s | Thickness: %s | Dimension: %s",
				order.OrderNumber, order.SKUID, order.Thickness, order.Dimension)
			pdf.Cell(0, 8, text)
			y += 10
		}
	}

	return pdf.OutputFileAndClose(outputPDF)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func saveFile(src io.Reader, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}
