package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"ai-corp/pkg/problem"
	"ai-corp/pkg/rag"
)

func main() {
	// 子命令
	ingestCmd := flag.NewFlagSet("ingest", flag.ExitOnError)
	searchCmd := flag.NewFlagSet("search", flag.ExitOnError)
	getCmd := flag.NewFlagSet("get", flag.ExitOnError)
	hintCmd := flag.NewFlagSet("hint", flag.ExitOnError)
	statsCmd := flag.NewFlagSet("stats", flag.ExitOnError)

	// 默认题目目录
	defaultProblemsDir := "problems"

	// ingest 命令参数
	ingestDir := ingestCmd.String("dir", defaultProblemsDir, "题目目录")
	ingestFile := ingestCmd.String("file", "", "单个题目文件")
	ingestAPIKey := ingestCmd.String("api-key", "", "DeepSeek API Key (可选，不提供则使用模拟向量)")

	// search 命令参数
	searchQuery := searchCmd.String("query", "", "搜索查询")
	searchTopK := searchCmd.Int("top-k", 5, "返回数量")
	searchDir := searchCmd.String("dir", defaultProblemsDir, "题目目录（用于加载已有题目）")

	// get 命令参数
	getID := getCmd.String("id", "", "题目ID")
	getDir := getCmd.String("dir", defaultProblemsDir, "题目目录")

	// hint 命令参数
	hintID := hintCmd.String("id", "", "题目ID")
	hintLevel := hintCmd.Int("level", 1, "提示级别 1-3")
	hintDir := hintCmd.String("dir", defaultProblemsDir, "题目目录")

	// stats 命令参数
	statsDir := statsCmd.String("dir", defaultProblemsDir, "题目目录")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// 初始化服务
	var embedClient rag.EmbeddingClient
	var store rag.VectorStore
	var ragService *rag.RAGService

	// 使用内存存储和模拟向量（生产环境可替换为真实实现）
	store = rag.NewMemoryVectorStore()
	embedClient = rag.NewMockEmbeddingClient()

	// 如果提供了 API Key，使用真实的 Embedding 服务
	if *ingestAPIKey != "" {
		embedClient = rag.NewDeepSeekEmbeddingClient(*ingestAPIKey)
	}

	ragService = rag.NewRAGService(store, embedClient)

	switch os.Args[1] {
	case "ingest":
		ingestCmd.Parse(os.Args[2:])
		handleIngest(ingestCmd, ragService, *ingestDir, *ingestFile)

	case "search":
		searchCmd.Parse(os.Args[2:])
		handleSearch(searchCmd, ragService, *searchQuery, *searchTopK, *searchDir)

	case "get":
		getCmd.Parse(os.Args[2:])
		// 先加载已有题目
		loadProblems(ragService, *getDir)
		handleGet(getCmd, ragService, *getID)

	case "hint":
		hintCmd.Parse(os.Args[2:])
		// 先加载已有题目
		loadProblems(ragService, *hintDir)
		handleHint(hintCmd, ragService, *hintID, *hintLevel)

	case "stats":
		statsCmd.Parse(os.Args[2:])
		// 先加载已有题目
		loadProblems(ragService, *statsDir)
		handleStats(statsCmd, ragService)

	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("题目管理 CLI")
	fmt.Println()
	fmt.Println("用法:")
	fmt.Println("  problem-cli ingest [选项]    入库题目")
	fmt.Println("  problem-cli search [选项]    搜索相似题目")
	fmt.Println("  problem-cli get [选项]       获取题目详情")
	fmt.Println("  problem-cli hint [选项]      获取解题提示")
	fmt.Println("  problem-cli stats            查看统计信息")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  # 入库所有题目")
	fmt.Println("  problem-cli ingest -dir problems")
	fmt.Println()
	fmt.Println("  # 入库单个题目")
	fmt.Println("  problem-cli ingest -file problems/001-two-sum.yaml")
	fmt.Println()
	fmt.Println("  # 搜索相似题目")
	fmt.Println("  problem-cli search -query \"两数之和\"")
	fmt.Println()
	fmt.Println("  # 获取题目详情")
	fmt.Println("  problem-cli get -id 001")
	fmt.Println()
	fmt.Println("  # 获取解题提示")
	fmt.Println("  problem-cli hint -id 001 -level 2")
}

// loadProblems 加载题目到服务（不进行向量化）
func loadProblems(ragService *rag.RAGService, dir string) {
	problems, err := problem.LoadAllProblems(dir)
	if err != nil {
		return // 静默失败
	}
	for _, p := range problems {
		ragService.GetProblem(p.ID) // 这会触发加载
	}
	// 直接加载到 problemStore
	ragService.LoadProblems(problems)
}

// loadAndIngest 加载题目并进行向量化入库
func loadAndIngest(ctx context.Context, ragService *rag.RAGService, dir string) {
	problems, err := problem.LoadAllProblems(dir)
	if err != nil {
		return
	}
	for _, p := range problems {
		ragService.IngestProblem(ctx, p)
	}
}

func handleIngest(cmd *flag.FlagSet, ragService *rag.RAGService, dir, file string) {
	ctx := context.Background()

	if file != "" {
		// 入库单个文件
		p, err := problem.ParseYAML(file)
		if err != nil {
			log.Fatalf("解析文件失败: %v", err)
		}
		if err := ragService.IngestProblem(ctx, p); err != nil {
			log.Fatalf("入库失败: %v", err)
		}
		fmt.Printf("入库成功: %s - %s\n", p.ID, p.Title)
	} else {
		// 入库整个目录
		if err := ragService.IngestFromDir(ctx, dir); err != nil {
			log.Fatalf("入库失败: %v", err)
		}
	}
}

func handleSearch(cmd *flag.FlagSet, ragService *rag.RAGService, query string, topK int, dir string) {
	if query == "" {
		log.Fatal("请提供搜索查询: -query")
	}

	ctx := context.Background()

	// 先加载已有题目并入库
	loadAndIngest(ctx, ragService, dir)

	results, err := ragService.SearchSimilarProblems(ctx, query, topK)
	if err != nil {
		log.Fatalf("搜索失败: %v", err)
	}

	if len(results) == 0 {
		fmt.Println("未找到相似题目")
		return
	}

	fmt.Printf("找到 %d 道相似题目:\n\n", len(results))
	for i, result := range results {
		title := "未知题目"
		if t, ok := result.Metadata["title"].(string); ok {
			title = t
		}
		difficulty := "unknown"
		if d, ok := result.Metadata["difficulty"].(string); ok {
			difficulty = d
		}

		fmt.Printf("%d. [%s] %s (ID: %s, 相似度: %.2f)\n",
			i+1, difficulty, title, result.ID, result.Score)
	}
}

func handleGet(cmd *flag.FlagSet, ragService *rag.RAGService, id string) {
	if id == "" {
		log.Fatal("请提供题目ID: -id")
	}

	p := ragService.GetProblem(id)
	if p == nil {
		log.Fatalf("题目不存在: %s", id)
	}

	fmt.Printf("题目 %s: %s\n", p.ID, p.Title)
	fmt.Printf("难度: %s\n", p.Difficulty)
	fmt.Printf("标签: %v\n\n", p.Tags)
	fmt.Printf("描述:\n%s\n\n", p.Description)

	if len(p.Examples) > 0 {
		fmt.Println("示例:")
		for i, ex := range p.Examples {
			fmt.Printf("  示例 %d:\n", i+1)
			fmt.Printf("    输入: %s\n", ex.Input)
			fmt.Printf("    输出: %s\n", ex.Output)
			if ex.Explanation != "" {
				fmt.Printf("    解释: %s\n", ex.Explanation)
			}
		}
	}

	if len(p.Constraints) > 0 {
		fmt.Println("\n约束条件:")
		for _, c := range p.Constraints {
			fmt.Printf("  - %s\n", c)
		}
	}
}

func handleHint(cmd *flag.FlagSet, ragService *rag.RAGService, id string, level int) {
	if id == "" {
		log.Fatal("请提供题目ID: -id")
	}

	hint, err := ragService.GetHint(id, level)
	if err != nil {
		log.Fatalf("获取提示失败: %v", err)
	}

	fmt.Println(hint)
}

func handleStats(cmd *flag.FlagSet, ragService *rag.RAGService) {
	stats := ragService.GetStats()

	fmt.Println("题库统计信息:")
	fmt.Println()

	if total, ok := stats["total"].(int); ok {
		fmt.Printf("题目总数: %d\n\n", total)
	}

	if byDifficulty, ok := stats["by_difficulty"].(map[string]int); ok {
		fmt.Println("按难度分布:")
		fmt.Printf("  简单: %d\n", byDifficulty["easy"])
		fmt.Printf("  中等: %d\n", byDifficulty["medium"])
		fmt.Printf("  困难: %d\n", byDifficulty["hard"])
	}

	if byTag, ok := stats["by_tag"].(map[string]int); ok {
		fmt.Println("\n按标签分布:")
		for tag, count := range byTag {
			fmt.Printf("  %s: %d\n", tag, count)
		}
	}
}

// 获取当前可执行文件所在目录
func getExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}
