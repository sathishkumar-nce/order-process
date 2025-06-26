#!/bin/bash
echo "🚀 Starting PDF & SKU Processor Server..."
echo "🌐 Server will be available at: http://localhost:8080"
echo "📂 Upload directory: ./uploads"
echo "📁 Output directory: ./outputs"
echo "⏹️  Press Ctrl+C to stop the server"
echo ""

# Install dependencies
echo "📦 Installing Go dependencies..."
go mod tidy

# Check for required external tools
echo "🔍 Checking for required tools..."

# Check for pdftotext (needed for PDF processing)
if ! command -v pdftotext &> /dev/null; then
    echo "⚠️  Warning: pdftotext not found. PDF processing will not work."
    echo "📥 Install poppler-utils:"
    echo "   macOS: brew install poppler"
    echo "   Ubuntu: sudo apt-get install poppler-utils"
    echo "   Windows: Download from https://github.com/oschwartz10612/poppler-windows/releases/"
    echo ""
else
    echo "✅ pdftotext found"
fi

# Check for pdftk (needed for PDF overlay, optional)
if ! command -v pdftk &> /dev/null; then
    echo "⚠️  Warning: pdftk not found. PDF overlay will use simple overlay."
    echo "📥 Install pdftk (optional):"
    echo "   macOS: brew install pdftk-java"
    echo "   Ubuntu: sudo apt-get install pdftk"
    echo ""
else
    echo "✅ pdftk found"
fi

echo "🚀 Starting web server..."
go run main.go
