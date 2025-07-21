# Tinkerbell Web UI

This is the Tinkerbell dashboard web interface built with [templ](https://github.com/a-h/templ), a Go templating library that compiles to Go code.

## Prerequisites

- Go 1.24.1 or later
- Make (optional, for convenience commands)

## Getting Started

### 1. Install Dependencies

```bash
go mod tidy
```

### 2. Generate Templates

The templ files need to be compiled to Go code:

```bash
# Using make
make generate

# Or directly with go run
go run github.com/a-h/templ/cmd/templ@latest generate
```

### 3. Run the Development Server

```bash
# Using make
make dev

# Or directly
go run ./cmd/server
```

The server will start on `http://localhost:8080`.

## Project Structure

```
web/
├── cmd/server/          # Main server application
│   └── main.go
├── artwork/             # Static assets (logos, images)
├── dashboard.templ      # Main dashboard template
├── dashboard_templ.go   # Generated Go code (do not edit)
├── icons.go            # SVG icon functions
├── Makefile            # Build commands
├── go.mod              # Go module definition
└── README.md           # This file
```

## Available Make Commands

- `make generate` - Generate Go code from templ files
- `make build` - Build the server binary
- `make dev` - Generate templates and run development server
- `make clean` - Remove generated files and binaries
- `make watch` - Watch for changes and auto-reload (requires templ CLI)

## Features

- 🌙 Dark/Light mode toggle with localStorage persistence
- 📱 Responsive design with mobile navigation menu
- 🔍 Global search functionality
- 🗂️ Collapsible navigation with BMC dropdown
- 📱 Mobile-first sidebar with overlay and hamburger menu
- 🎨 Themed logo switching
- ⚡ Server-side rendering with templ

## Development

### Adding New Templates

1. Create a new `.templ` file
2. Define your template components using templ syntax
3. Run `make generate` to compile to Go
4. Import and use in your server routes

### Template Syntax

templ uses Go-like syntax for templates. Key features:

- `{ variable }` - Output variables
- `@ComponentName()` - Render components
- `if condition { }` - Conditional rendering
- `for item := range items { }` - Loops
- `@templ.Raw(htmlString)` - Render raw HTML

Example:
```templ
templ MyComponent(title string, items []string) {
    <h1>{ title }</h1>
    <ul>
        for _, item := range items {
            <li>{ item }</li>
        }
    </ul>
}
```

### Static Assets

Static files are served from:
- `/artwork` → `./artwork/` (logos, images)
- `/css` → `./` (CSS files)

## Migration from HTML

This project was migrated from a static HTML file to a templ-based architecture:

- **Before**: Single `index.html` file with embedded CSS and JavaScript
- **After**: Modular templ components with Go server
- **Benefits**: 
  - Server-side rendering
  - Component reusability
  - Type safety
  - Better maintainability
  - Integration with Go ecosystem

## Deployment

### Build for Production

```bash
make build
```

This creates a `bin/server` binary that can be deployed.

### Docker (Optional)

You can containerize the application:

```dockerfile
FROM golang:1.24.1-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN go run github.com/a-h/templ/cmd/templ@latest generate
RUN go build -o server ./cmd/server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/server .
COPY --from=builder /app/artwork ./artwork
COPY --from=builder /app/*.css ./
EXPOSE 8080
CMD ["./server"]
```

## Contributing

1. Make changes to `.templ` files (not the generated `_templ.go` files)
2. Run `make generate` to update generated code
3. Test your changes with `make dev`
4. Commit both `.templ` and generated files
