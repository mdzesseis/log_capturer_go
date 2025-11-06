package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v2"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("🔍 SIMPLE FILE MONITOR VALIDATION TEST")
	fmt.Println("========================================\n")

	// Ler arquivo de pipeline
	pipelineFile := "/home/mateus/log_capturer_go/configs/file_pipeline.yml"
	fmt.Printf("📋 Reading pipeline config: %s\n", pipelineFile)

	data, err := ioutil.ReadFile(pipelineFile)
	if err != nil {
		log.Fatalf("Failed to read pipeline file: %v", err)
	}

	var pipelineConfig map[string]interface{}
	err = yaml.Unmarshal(data, &pipelineConfig)
	if err != nil {
		log.Fatalf("Failed to parse pipeline config: %v", err)
	}

	// Verificar arquivos configurados
	fmt.Println("\n📁 Files configured in pipeline:")
	filesFound := 0
	filesExist := 0

	if filesInterface, ok := pipelineConfig["files"]; ok {
		if files, ok := filesInterface.([]interface{}); ok {
			for _, fileInterface := range files {
				if file, ok := fileInterface.(map[interface{}]interface{}); ok {
					if pathInterface, ok := file["path"]; ok {
						if path, ok := pathInterface.(string); ok {
							filesFound++

							// Verificar se arquivo existe
							if _, err := os.Stat(path); err == nil {
								fmt.Printf("   ✅ %s (EXISTS)\n", path)
								filesExist++
							} else if os.IsNotExist(err) {
								fmt.Printf("   ❌ %s (NOT FOUND)\n", path)
							} else {
								fmt.Printf("   ⚠️  %s (ERROR: %v)\n", path, err)
							}

							// Mostrar labels configuradas
							if labelsInterface, ok := file["labels"]; ok {
								if labels, ok := labelsInterface.(map[interface{}]interface{}); ok {
									fmt.Println("      Labels:")
									for k, v := range labels {
										fmt.Printf("        - %v: %v\n", k, v)
									}
								}
							}

							// Verificar se está habilitado
							if enabledInterface, ok := file["enabled"]; ok {
								if enabled, ok := enabledInterface.(bool); ok {
									if enabled {
										fmt.Println("      Status: ENABLED ✅")
									} else {
										fmt.Println("      Status: DISABLED ❌")
									}
								}
							} else {
								fmt.Println("      Status: ENABLED (default) ✅")
							}
						}
					}
				}
			}
		}
	}

	fmt.Printf("\n📊 Summary:\n")
	fmt.Printf("   Total files configured: %d\n", filesFound)
	fmt.Printf("   Files that exist: %d\n", filesExist)
	fmt.Printf("   Files missing: %d\n", filesFound-filesExist)

	// Criar arquivo de teste
	testFile := "/tmp/test_file_monitor.log"
	fmt.Printf("\n📝 Creating test file: %s\n", testFile)

	err = ioutil.WriteFile(testFile, []byte("Test log entry created at "+time.Now().Format(time.RFC3339)+"\n"), 0644)
	if err != nil {
		fmt.Printf("❌ Failed to create test file: %v\n", err)
	} else {
		fmt.Printf("✅ Test file created successfully\n")
	}

	// Verificar se o arquivo seria monitorado baseado na configuração
	fmt.Println("\n🔍 Checking if test file would be monitored...")

	// Verificar diretórios configurados
	if dirsInterface, ok := pipelineConfig["directories"]; ok {
		if dirs, ok := dirsInterface.([]interface{}); ok {
			testDir := filepath.Dir(testFile)
			wouldBeMonitored := false

			fmt.Printf("   Test file directory: %s\n", testDir)
			fmt.Println("   Configured directories:")

			for _, dirInterface := range dirs {
				switch dir := dirInterface.(type) {
				case string:
					fmt.Printf("      - %s", dir)
					if dir == testDir || dir == "/tmp" {
						fmt.Printf(" ✅ (MATCHES)")
						wouldBeMonitored = true
					}
					fmt.Println()
				case map[interface{}]interface{}:
					if pathInterface, ok := dir["path"]; ok {
						if path, ok := pathInterface.(string); ok {
							fmt.Printf("      - %s", path)
							if path == testDir || path == "/tmp" {
								fmt.Printf(" ✅ (MATCHES)")
								wouldBeMonitored = true
							}
							fmt.Println()
						}
					}
				}
			}

			if wouldBeMonitored {
				fmt.Println("   ✅ Test file WOULD be monitored (directory matches)")
			} else {
				fmt.Println("   ❌ Test file would NOT be monitored (no directory match)")
				fmt.Println("   💡 Add '/tmp' to directories in pipeline config to monitor test file")
			}
		}
	} else {
		fmt.Println("   ⚠️  No directories configured in pipeline")
	}

	// Verificar configuração do file_monitor_service
	fmt.Println("\n📋 Checking file_monitor_service configuration:")

	configFile := "/home/mateus/log_capturer_go/configs/config.yaml"
	configData, err := ioutil.ReadFile(configFile)
	if err != nil {
		fmt.Printf("   ❌ Failed to read config.yaml: %v\n", err)
	} else {
		var config map[string]interface{}
		err = yaml.Unmarshal(configData, &config)
		if err != nil {
			fmt.Printf("   ❌ Failed to parse config.yaml: %v\n", err)
		} else {
			if fileMonitorService, ok := config["file_monitor_service"].(map[interface{}]interface{}); ok {
				if enabled, ok := fileMonitorService["enabled"].(bool); ok {
					if enabled {
						fmt.Println("   ✅ file_monitor_service is ENABLED")
					} else {
						fmt.Println("   ❌ file_monitor_service is DISABLED")
					}
				}

				if pipelineFile, ok := fileMonitorService["pipeline_file"].(string); ok {
					fmt.Printf("   📁 Pipeline file: %s\n", pipelineFile)
				}
			}
		}
	}

	// Sugestão de teste completo
	fmt.Println("\n💡 To fully test the file monitor:")
	fmt.Println("   1. Ensure file_monitor_service is enabled in config.yaml")
	fmt.Println("   2. Add files to monitor in file_pipeline.yml")
	fmt.Println("   3. Run the application: go run cmd/main.go")
	fmt.Println("   4. Check logs for 'Processing file pipeline configuration'")
	fmt.Println("   5. Look for 'Added specific file from pipeline' messages")
	fmt.Println("   6. Write to monitored files and check if logs are captured")

	// Criar script de teste
	scriptFile := "/tmp/test_file_monitor.sh"
	script := `#!/bin/bash
echo "Testing file monitor..."

# Create test log file
TEST_FILE="/var/log/test_monitor.log"
echo "Creating test file: $TEST_FILE"
sudo touch $TEST_FILE
sudo chmod 644 $TEST_FILE

# Write test entries
for i in {1..5}; do
    echo "[$(date)] Test log entry $i" | sudo tee -a $TEST_FILE
    sleep 1
done

echo "Test complete. Check application logs for captured entries."
`

	err = ioutil.WriteFile(scriptFile, []byte(script), 0755)
	if err == nil {
		fmt.Printf("\n📝 Test script created: %s\n", scriptFile)
		fmt.Println("   Run it while the application is running to test file monitoring")
	}

	fmt.Println("\n✅ Validation complete!")
}