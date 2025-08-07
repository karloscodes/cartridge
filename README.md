# 🎮 Cartridge - Go Web Framework

> **Ultra-clean Go web development with sublime developer experience**

⚠️ Work in Progress - This module is under active development and APIs may change.

An **opinionated**, **batteries-included** Go web framework built on [Fiber](https://gofiber.io) that prioritizes developer experience above all else. Write less boilerplate, ship faster.

## ✨ Why Cartridge?

```go
// ❌ Before: Traditional Go web development
func main() {
    // 50+ lines of setup boilerplate...
    if err := server.Start(":8080"); err != nil {
        log.Fatal(err)
    }
}

// ✅ After: Cartridge magic
func main() {
    app := cartridge.NewFullStack()
    
    app.Get("/users", func(ctx *cartridge.Context) error {
        return ctx.JSON([]User{}) // Just works! 
    })
    
    app.Run()
}
```

## 🚀 Features That Make You Smile

- **🎯 One-line everything** - Server, database, migrations, cron jobs
- **🎨 Structured logging** - Beautiful, structured logging
- **⚡ Async processing** - Background tasks with zero ceremony  
- **⏰ Cron jobs** - Scheduled tasks with database access
- **🛡️ Smart error handling** - Panics for setup, returns for runtime
- **🎪 Context magic** - One parameter handlers with everything you need
- **📁 Embedded assets** - Migrations, templates, static files

## 📦 Quick Start

```bash
go mod init my-app
go get github.com/karloscodes/cartridge
```

**Create `main.go`:**

```go
package main

import "github.com/karloscodes/cartridge"

func main() {
    app := cartridge.NewFullStack()
    
    // Ultra-clean handlers - one parameter only!
    app.Get("/", func(ctx *cartridge.Context) error {
        ctx.Logger.Info("🏠 Home page visited", 
            "endpoint", "/",
            "user_agent", ctx.Fiber.Get("User-Agent"))
            
        return ctx.JSON(map[string]string{
            "message": "🚀 Welcome to Cartridge!",
            "status":  "awesome",
        })
    })
    
    app.Run() // That's it! Server, DB, migrations - all handled
}
```

```bash
go run main.go
```

**Console output with beautiful colors:**
```
15:04:05 [INFO ] Starting Cartridge application service=my-app version=1.0.0
15:04:05 [INFO ] Setting up routes
15:04:05 [INFO ] Server ready status=listening port=8080
15:04:12 [INFO ] Home page visited endpoint=/ user_agent=curl/7.68.0
```

## 🎮 Try the Interactive Demo

```bash
# Clone and run the demo
git clone https://github.com/karloscodes/cartridge
cd cartridge/simple-demo
go run main.go

# Test the endpoints
curl http://localhost:3000/hello?name=Developer
curl -X POST http://localhost:3000/demo/async
curl http://localhost:3000/status/demo-task
```

## ⚡ Async Processing Made Simple

Background tasks with **zero ceremony**:

```go
// Define your async handler
func ProcessLargeDataset(ctx *cartridge.AsyncContext, args map[string]interface{}) (interface{}, error) {
    ctx.Logger.Info("🚀 Starting processing", "task_id", ctx.TaskID)
    
    // Your processing logic...
    time.Sleep(5 * time.Second)
    
    return map[string]interface{}{
        "processed_records": 10000,
        "status": "success",
    }, nil
}

// Start background task
app.Post("/process", func(ctx *cartridge.Context) error {
    taskID, err := app.AsyncJob("process-data", ProcessLargeDataset, map[string]interface{}{
        "dataset_size": "large",
        "priority": "high",
    })
    
    return ctx.JSON(map[string]string{"task_id": taskID})
})

// Check task status  
app.Get("/status/:id", func(ctx *cartridge.Context) error {
    task, err := app.AsyncStatus(ctx.Params("id"))
    return ctx.JSON(task)
})
```

## ⏰ Cron Jobs with Database Access

Schedule tasks with **the same clean UX** as HTTP handlers:

```go
func CleanupOldData(ctx *cartridge.CronContext) error {
    ctx.Logger.Info("🧹 Starting cleanup job")
    
    // Direct database access - no setup needed!
    result := ctx.DBExec("DELETE FROM logs WHERE created_at < datetime('now', '-30 days')")
    
    ctx.Logger.Info("✅ Cleanup completed", "rows_affected", result.RowsAffected)
    return nil
}

// Register with optional description
app.CronJob("cleanup", "0 2 * * *", CleanupOldData, "Clean old logs daily")
app.CronJob("reports", "0 9 * * MON", GenerateWeeklyReport) // No description needed
```

## 🛡️ Smart Error Handling

**Setup errors panic** (you want to know immediately), **runtime errors return** (handle gracefully):

```go
// Setup errors panic - fail fast!
app.CronJob("invalid", "invalid-cron", handler) // 💥 Panics immediately
app.Run() // 💥 Panics if can't bind port

// Schedule a task -> Runtime errors return - handle gracefully  
taskID, err := app.AsyncJob("task", handler, args)
if err != nil {
    return ctx.BadRequest(err, "Failed to start task")
}
```

## 🎯 Design Philosophy

1. **Developer Experience First** - If it's not delightful, we fix it
2. **Convention over Configuration** - Sensible defaults, override when needed  
4. **Fail Fast in Development** - Panics for setup, returns for runtime
5. **Batteries Included** - A cartridge containing all you need: Database, migrations, cron, async jobs, web helpers - all built-in


## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.
