# PDF & SKU Processor

A unified web application for processing PDF invoices and extracting SKU data with mapping functionality.

## Features

### 📄 PDF Processor
- Extract order data from PDF invoices
- Map SKUs to thickness and dimension data
- Generate CSV reports or PDF overlays
- Support for Amazon invoice format

### 🏷️ SKU Extractor  
- Extract SKUs from text files
- Map to thickness, dimension, and weight data
- Generate detailed CSV reports with statistics
- Support for bulk SKU processing

## Quick Start

1. **Setup the project:**
   ```bash
   ./setup.sh
   ```

2. **Start the server:**
   ```bash
   ./run.sh
   ```

3. **Open your browser:**
   Navigate to http://localhost:8080

## Usage

### PDF Processing
1. Upload a PDF invoice file
2. Upload an Excel SKU mapping file (SKU, Thickness, Dimension columns)
3. Choose output mode:
   - **CSV**: Extract data to spreadsheet
   - **PDF Overlay**: Annotate original PDF
4. Download the processed file

### SKU Extraction
1. Upload a text file containing SKU references
2. Upload an Excel mapping file (SKU, Thickness, Dimension, Weight columns)
3. Get a detailed CSV report with match statistics

## File Formats

### SKU Mapping Excel File
| SKU | Thickness | Dimension | Weight |
|-----|-----------|-----------|---------|
| MRC-MR-0530 | 2mm | 24 x 48 Inch | 0.360 |
| MRC-MR-0376 | 1.5mm | 35 x 54 Inch | 0.420 |

### Output CSV Format
- Order Number, SKU ID, Thickness, Dimension, Page Number (PDF)
- SKU, Thickness, Dimension, Weight, Status (SKU Extractor)

## Requirements

- Go 1.22.4 or later
- poppler-utils (for PDF text extraction)
- pdftk (optional, for advanced PDF overlay)

### Installing Dependencies

**macOS:**
```bash
brew install poppler pdftk-java
```

**Ubuntu:**
```bash
sudo apt-get install poppler-utils pdftk
```

**Windows:**
- Download poppler from: https://github.com/oschwartz10612/poppler-windows/releases/
- Download pdftk from: https://www.pdflabs.com/tools/pdftk-the-pdf-toolkit/

## Project Structure

```
pdf-sku-processor/
├── main.go                 # Web server
├── handlers/
│   ├── pdf_processor.go    # PDF processing logic
│   └── sku_extractor.go    # SKU extraction logic
├── uploads/                # Temporary file storage
├── outputs/                # Generated files
├── go.mod                  # Go dependencies
└── README.md               # This file
```

## API Endpoints

- `GET /` - Web interface
- `POST /process-pdf` - Process PDF files
- `POST /process-sku` - Process SKU files  
- `GET /outputs/{filename}` - Download generated files

## Troubleshooting

### PDF Processing Issues
- Ensure pdftotext is installed and in PATH
- Check PDF file is not password protected
- Verify SKU mapping Excel file format

### SKU Extraction Issues
- Check text file contains "SKU: [value]" format
- Verify Excel mapping file has correct columns
- Ensure file uploads are under 10MB

## Support

For issues and questions, check the console output for detailed error messages.
