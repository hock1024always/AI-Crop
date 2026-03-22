package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"ai-corp/pkg/skill"
)

func main() {
	fmt.Println("=== Testing Enhanced Skills ===")
	
	// 创建 Skill 注册表
	registry := skill.NewRegistry()
	
	// 测试各个 Skills
	ctx := context.Background()
	
	// 1. 测试 code_generation
	fmt.Println("\n1. Testing code_generation skill:")
	result, err := registry.Execute(ctx, "code_generation", map[string]interface{}{
		"language":    "python",
		"requirement": "写一个计算斐波那契数列的函数，要求处理边界情况",
	})
	if err != nil {
		log.Printf("Code generation failed: %v", err)
	} else {
		printResult("Code Generation", result)
	}
	
	// 2. 测试 code_review
	fmt.Println("\n2. Testing code_review skill:")
	code := `def fib(n):
    if n <= 1:
        return n
    return fib(n-1) + fib(n-2)`
	
	result, err = registry.Execute(ctx, "code_review", map[string]interface{}{
		"code":     code,
		"language": "python",
	})
	if err != nil {
		log.Printf("Code review failed: %v", err)
	} else {
		printResult("Code Review", result)
	}
	
	// 3. 测试 debug
	fmt.Println("\n3. Testing debug skill:")
	errorCode := `def divide(a, b):
    return a / b`
	
	result, err = registry.Execute(ctx, "debug", map[string]interface{}{
		"code":     errorCode,
		"error":    "ZeroDivisionError: division by zero",
		"language": "python",
	})
	if err != nil {
		log.Printf("Debug failed: %v", err)
	} else {
		printResult("Debug", result)
	}
	
	// 4. 测试 test_generation
	fmt.Println("\n4. Testing test_generation skill:")
	result, err = registry.Execute(ctx, "test_generation", map[string]interface{}{
		"code":      code,
		"test_type": "unit",
		"language":  "python",
	})
	if err != nil {
		log.Printf("Test generation failed: %v", err)
	} else {
		printResult("Test Generation", result)
	}
	
	// 5. 测试 system_design
	fmt.Println("\n5. Testing system_design skill:")
	result, err = registry.Execute(ctx, "system_design", map[string]interface{}{
		"requirements": "构建一个在线购物网站，支持商品浏览、购物车、订单管理",
		"constraints":  "需要支持高并发，预算有限",
	})
	if err != nil {
		log.Printf("System design failed: %v", err)
	} else {
		printResult("System Design", result)
	}
	
	// 6. 测试 deploy
	fmt.Println("\n6. Testing deploy skill:")
	result, err = registry.Execute(ctx, "deploy", map[string]interface{}{
		"artifact":    "my-web-app:v1.0",
		"environment": "production",
		"platform":    "kubernetes",
	})
	if err != nil {
		log.Printf("Deploy failed: %v", err)
	} else {
		printResult("Deploy", result)
	}
	
	// 7. 测试 api_call
	fmt.Println("\n7. Testing api_call skill:")
	result, err = registry.Execute(ctx, "api_call", map[string]interface{}{
		"url":    "https://httpbin.org/get",
		"method": "GET",
	})
	if err != nil {
		log.Printf("API call failed: %v", err)
	} else {
		printResult("API Call", result)
	}
	
	// 8. 测试 db_query
	fmt.Println("\n8. Testing db_query skill:")
	result, err = registry.Execute(ctx, "db_query", map[string]interface{}{
		"sql": "SELECT id, name, price FROM products WHERE category = 'electronics'",
	})
	if err != nil {
		log.Printf("DB query failed: %v", err)
	} else {
		printResult("DB Query", result)
	}
	
	// 9. 测试 file_operation
	fmt.Println("\n9. Testing file_operation skill:")
	
	// 写文件
	result, err = registry.Execute(ctx, "file_operation", map[string]interface{}{
		"action":  "write",
		"path":    "test.txt",
		"content": "Hello, AI Corp!",
		"workspace_dir": "/tmp/test-workspace",
	})
	if err != nil {
		log.Printf("File write failed: %v", err)
	} else {
		printResult("File Write", result)
	}
	
	// 读文件
	result, err = registry.Execute(ctx, "file_operation", map[string]interface{}{
		"action":  "read",
		"path":    "test.txt",
		"workspace_dir": "/tmp/test-workspace",
	})
	if err != nil {
		log.Printf("File read failed: %v", err)
	} else {
		printResult("File Read", result)
	}
	
	// 10. 测试 code_search
	fmt.Println("\n10. Testing code_search skill:")
	result, err = registry.Execute(ctx, "code_search", map[string]interface{}{
		"query":    "fibonacci",
		"language": "python",
		"workspace_dir": "/tmp/test-workspace",
	})
	if err != nil {
		log.Printf("Code search failed: %v", err)
	} else {
		printResult("Code Search", result)
	}
	
	fmt.Println("\n=== Skills Testing Completed ===")
}

func printResult(title string, result map[string]interface{}) {
	fmt.Printf("--- %s Result ---\n", title)
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}