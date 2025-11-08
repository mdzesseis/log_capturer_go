#!/bin/bash
# Goroutine Leak Monitor
# Uses existing /debug/goroutines endpoint to detect and analyze leaks

set -e

API_URL="http://localhost:8401"
DURATION_MIN=${1:-10}
SAMPLE_INTERVAL=${2:-30}

SAMPLES=$((DURATION_MIN * 60 / SAMPLE_INTERVAL))

echo "=== Goroutine Leak Monitor ==="
echo "Duration: ${DURATION_MIN} minutes"
echo "Sample interval: ${SAMPLE_INTERVAL} seconds"
echo "Total samples: ${SAMPLES}"
echo ""

# Create output files
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_DIR="/tmp/goroutine_leak_analysis_${TIMESTAMP}"
mkdir -p "$OUTPUT_DIR"

RAW_DATA="$OUTPUT_DIR/goroutine_data.csv"
ANALYSIS="$OUTPUT_DIR/analysis.txt"

echo "timestamp,goroutines,alloc_mb,total_alloc_mb,sys_mb,num_gc" > "$RAW_DATA"

# Capture baseline
echo "Capturing baseline..."
BASELINE=$(curl -s "$API_URL/debug/goroutines" | jq -r '.goroutines')
echo "Baseline goroutines: $BASELINE"
echo ""

# Monitor loop
echo "Monitoring for $DURATION_MIN minutes..."
for i in $(seq 1 $SAMPLES); do
    TIMESTAMP_NOW=$(date +"%Y-%m-%d %H:%M:%S")

    # Fetch debug data
    DEBUG_DATA=$(curl -s "$API_URL/debug/goroutines")

    GOROUTINES=$(echo "$DEBUG_DATA" | jq -r '.goroutines')
    ALLOC=$(echo "$DEBUG_DATA" | jq -r '.memory.alloc')
    TOTAL_ALLOC=$(echo "$DEBUG_DATA" | jq -r '.memory.total_alloc')
    SYS=$(echo "$DEBUG_DATA" | jq -r '.memory.sys')
    NUM_GC=$(echo "$DEBUG_DATA" | jq -r '.memory.num_gc')

    # Convert to MB
    ALLOC_MB=$((ALLOC / 1024 / 1024))
    TOTAL_ALLOC_MB=$((TOTAL_ALLOC / 1024 / 1024))
    SYS_MB=$((SYS / 1024 / 1024))

    # Calculate growth
    GROWTH=$((GOROUTINES - BASELINE))
    ELAPSED=$((i * SAMPLE_INTERVAL))
    RATE=$(echo "scale=2; ($GOROUTINES - $BASELINE) / ($ELAPSED / 60.0)" | bc 2>/dev/null || echo "0")

    # Write to CSV
    echo "$TIMESTAMP_NOW,$GOROUTINES,$ALLOC_MB,$TOTAL_ALLOC_MB,$SYS_MB,$NUM_GC" >> "$RAW_DATA"

    # Print status
    printf "[%2d/%2d] %s - Goroutines: %4d (growth: %+4d, rate: %6.2f/min) | Mem: %4d MB\n" \
        "$i" "$SAMPLES" "$TIMESTAMP_NOW" "$GOROUTINES" "$GROWTH" "$RATE" "$ALLOC_MB"

    # Wait for next sample (unless last sample)
    if [ $i -lt $SAMPLES ]; then
        sleep $SAMPLE_INTERVAL
    fi
done

# Final capture
echo ""
echo "=== Final Analysis ==="
FINAL=$(curl -s "$API_URL/debug/goroutines" | jq -r '.goroutines')
TOTAL_GROWTH=$((FINAL - BASELINE))
LEAK_RATE=$(echo "scale=2; $TOTAL_GROWTH / $DURATION_MIN" | bc)

# Write analysis
{
    echo "=== Goroutine Leak Analysis Report ==="
    echo "Generated: $(date)"
    echo "Duration: $DURATION_MIN minutes"
    echo ""
    echo "=== Summary ==="
    echo "Baseline goroutines: $BASELINE"
    echo "Final goroutines: $FINAL"
    echo "Total growth: $TOTAL_GROWTH"
    echo "Leak rate: ${LEAK_RATE} goroutines/minute"
    echo ""

    if [ $TOTAL_GROWTH -gt 50 ]; then
        echo "⚠️  LEAK DETECTED (growth > 50 in $DURATION_MIN minutes)"
        echo ""
        echo "=== Recommended Actions ==="
        echo "1. Check container monitor: May have unclosed streams"
        echo "2. Check file monitor: May have goroutine leaks in polling"
        echo "3. Check dispatcher retry logic: May have orphaned retries"
        echo "4. Check HTTP client connections: May have connection leaks"
        echo ""
    elif [ $TOTAL_GROWTH -gt 10 ]; then
        echo "⚠️  MODERATE GROWTH (growth: $TOTAL_GROWTH)"
        echo "Continue monitoring to confirm leak"
        echo ""
    else
        echo "✅ NO LEAK DETECTED (growth within acceptable range)"
        echo ""
    fi

    echo "=== Growth Pattern ==="
    awk -F',' 'NR>1 {print $1, $2}' "$RAW_DATA" | tail -20
    echo ""

    echo "=== Memory Pattern ==="
    awk -F',' 'NR>1 {print $1, "Alloc:", $3"MB", "Sys:", $5"MB"}' "$RAW_DATA" | tail -10
    echo ""

    echo "Raw data saved to: $RAW_DATA"
    echo "Analysis saved to: $ANALYSIS"

} | tee "$ANALYSIS"

# Generate simple plot if gnuplot is available
if command -v gnuplot &> /dev/null; then
    PLOT_FILE="$OUTPUT_DIR/goroutine_growth.png"
    gnuplot <<EOF
set terminal png size 1200,600
set output '$PLOT_FILE'
set title 'Goroutine Growth Over Time'
set xlabel 'Time'
set ylabel 'Goroutines'
set xdata time
set timefmt "%Y-%m-%d %H:%M:%S"
set format x "%H:%M"
set grid
set datafile separator ","
plot '$RAW_DATA' using 1:2 with linespoints title 'Goroutines', \
     $BASELINE with lines linetype 2 title 'Baseline'
EOF
    echo "Plot saved to: $PLOT_FILE"
fi

echo ""
echo "Analysis complete. Output directory: $OUTPUT_DIR"
