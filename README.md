# Cartridge - Go Web Application Library

A comprehensive Go web application library built on Fiber framework, designed for rapid development of robust web applications with SQLite database support.

## Features

- **Fiber Application Framework**: Pre-configured Fiber app with essential middleware
- **Authentication System**: Cookie-based authentication with AES-GCM encryption
- **Database Layer**: SQLite-optimized connection management with retry logic
- **Configuration Management**: Environment-based configuration with sensible defaults
- **Advanced Middleware**: CSRF protection, rate limiting, concurrency control
- **Logging System**: Simple structured logging with file rotation
- **Static Assets & Templates**: Environment-aware asset serving and template engine
- **Testing Infrastructure**: Comprehensive testing utilities and helpers
- **Error Handling**: Global error handlers with environment-specific responses

## Quick Start

```go
package main

import (
    "github.com/karloscodes/cartridge/app"
    "github.com/karloscodes/cartridge/config"
    "github.com/karloscodes/cartridge/database"
    "github.com/karloscodes/cartridge/logging"
)

func main() {
    // Load configuration
    cfg := config.New()
    
    // Setup logging
    logger := logging.NewLogger(logging.LogConfig{
        Level:     cfg.LogLevel,
        Directory: cfg.LogsDirectory,
        MaxSize:   cfg.LogsMaxSizeInMb,
    })
    
    // Setup database
    dbManager := database.NewDBManager(cfg, logger)
    dbManager.Init()
    defer dbManager.Close()
    
    // Create Fiber app with all middleware
    fiberApp := app.NewFiberApp(app.FiberConfig{
        Environment: cfg.Environment,
        Port:       cfg.Port,
    }, dbManager, logger, cfg)
    
    // Start server
    logger.Fatal("Server failed to start", logging.Field{
        Key: "error", Value: fiberApp.Listen(":" + cfg.Port),
    })
}
```

## Environment Variables

```bash
APP_ENV=development          # development, production, test
APP_PORT=3000               # Server port
DATABASE_URL=data/app.db    # SQLite database path
PRIVATE_KEY=your-secret-key # Encryption key for sessions
DEBUG=true                  # Enable debug mode
LOG_LEVEL=info             # debug, info, warn, error
LOGS_DIR=logs              # Log files directory
```

## Package Structure

```
cartridge/
├── app/           # Fiber app factory and configuration
├── auth/          # Authentication system
├── config/        # Configuration management
├── database/      # Database connection management
├── middleware/    # All middleware implementations
├── logging/       # Simple logging setup and utilities
├── testing/       # Testing utilities and helpers
├── templates/     # Response rendering helpers
├── assets/        # Static asset management
└── utils/         # Common utilities and helpers
```

## License

MIT License - see LICENSE file for details.
