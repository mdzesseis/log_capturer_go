#!/bin/bash

echo "=========================================="
echo "📝 FILE MONITOR WRITE TEST"
echo "=========================================="

# Arquivo que será monitorado (existe e está configurado)
TEST_FILE="/var/log/dpkg.log"

echo ""
echo "📁 Testing with file: $TEST_FILE"
echo "   This file is configured and enabled in pipeline"
echo ""

# Fazer backup do arquivo original
if [ -f "$TEST_FILE" ]; then
    echo "📋 Creating backup of original file..."
    sudo cp "$TEST_FILE" "$TEST_FILE.backup"
fi

# Escrever entradas de teste
echo "✍️  Writing test entries to $TEST_FILE:"
for i in {1..3}; do
    TEST_MSG="$(date '+%Y-%m-%d %H:%M:%S') TEST: File monitor test entry $i - Checking if monitoring is working"
    echo "   Entry $i: $TEST_MSG"
    echo "$TEST_MSG" | sudo tee -a "$TEST_FILE" > /dev/null
    sleep 1
done

echo ""
echo "✅ Test entries written!"
echo ""
echo "📊 To verify monitoring is working:"
echo "   1. Make sure the application is running"
echo "   2. Check application logs for these messages"
echo "   3. Look for 'Dispatcher received' or similar log entries"
echo ""

# Mostrar últimas linhas do arquivo
echo "📋 Last 5 lines of $TEST_FILE:"
sudo tail -5 "$TEST_FILE"

echo ""
echo "✅ Test complete!"
echo ""
echo "💡 Note: Remember to restore the original file if needed:"
echo "   sudo mv $TEST_FILE.backup $TEST_FILE"
