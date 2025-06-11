# Tinkerbell UI

A modern web UI for managing Tinkerbell bare metal infrastructure, built with Go, Templ, Tailwind CSS, and HTMX.

## Features

- **Hardware Management**: Create, read, update, and delete hardware resources
- **Template Management**: Manage workflow templates with full CRUD operations
- **Workflow Management**: Monitor and control workflow executions
- **Modern UI**: Beautiful, responsive interface built with Tailwind CSS
- **Interactive**: HTMX-powered dynamic updates without page reloads
- **Development Mode**: Run with mock data for testing and development

## Requirements

- Go 1.24.1 or later
- Node.js and npm (for Tailwind CSS compilation)
- Access to a Kubernetes cluster with Tinkerbell CRDs (for production mode)

## Installation

1. Clone the Tinkerbell repository and navigate to the UI module:
   ```bash
   cd tinkerbell/ui
   ```

2. Install dependencies:
   ```bash
   go mod download
   npm install
   ```

3. Build the UI server:
   ```bash
   go build -o ui-server ./cmd
   ```

## Usage

### Development Mode

Run the UI with mock data for development and testing:

```bash
./ui-server -development=true -port=8080
```

This mode includes sample data:
- 2 hardware devices (server-01, server-02) 
- 2 templates (ubuntu-install, centos-install)
- 2 workflows (provision-server-01, provision-server-02)

### Production Mode

Run the UI connected to a Kubernetes cluster:

```bash
./ui-server -kubeconfig=/path/to/kubeconfig -namespace=tinkerbell-system -port=8080
```

For in-cluster deployment, omit the kubeconfig flag:

```bash
./ui-server -namespace=tinkerbell-system -port=8080
```

### Command Line Options

- `-port`: Port to listen on (default: 8080)
- `-kubeconfig`: Path to kubeconfig file (optional, uses in-cluster config if not provided)
- `-namespace`: Kubernetes namespace (default: "default")
- `-development`: Enable development mode with mock data (default: false)

## Pages

The UI provides 6 main pages:

1. **Home** (`/`): Dashboard with navigation to other sections
2. **Hardware List** (`/hardware`): View and create hardware resources
3. **Hardware Detail** (`/hardware/{name}`): View, edit, and delete specific hardware
4. **Template List** (`/templates`): View and create templates
5. **Template Detail** (`/templates/{name}`): View, edit, and delete specific templates
6. **Workflow List** (`/workflows`): View and create workflows
7. **Workflow Detail** (`/workflows/{name}`): View, edit, and delete specific workflows

## CRUD Operations

### Hardware
- **Create**: Use the "Create Hardware" button on the hardware list page
- **Read**: Click on any hardware item to view details
- **Update**: Use the "Edit" button on the hardware detail page
- **Delete**: Use the "Delete" button on the hardware detail page

### Templates
- **Create**: Use the "Create Template" button on the template list page
- **Read**: Click on any template item to view details
- **Update**: Use the "Edit" button on the template detail page
- **Delete**: Use the "Delete" button on the template detail page

### Workflows
- **Create**: Use the "Create Workflow" button on the workflow list page
- **Read**: Click on any workflow item to view details
- **Update**: Use the "Edit" button on the workflow detail page
- **Delete**: Use the "Delete" button on the workflow detail page

## Development

### Building CSS

If you modify the Tailwind CSS:

```bash
npm run build-css
```

### Generating Templates

If you modify `.templ` files, regenerate the Go code:

```bash
go run github.com/a-h/templ/cmd/templ generate
```

### Project Structure

```
ui/
├── cmd/main.go              # Entry point
├── main.go                  # Main server logic
├── server.go                # HTTP handlers and Kubernetes client
├── development.go           # Development mode with mock data
├── static.go                # Static file serving
├── *.templ                  # Templ template files
├── *_templ.go              # Generated Go files from templates
├── static/                  # Static assets (CSS, JS)
├── src/                     # Source CSS files
└── tools.go                # Go tools dependencies
```

## Architecture

The UI is built as a standalone Go module (`github.com/tinkerbell/tinkerbell/ui`) with the following technologies:

- **Go**: Backend server and Kubernetes client
- **Templ**: Type-safe HTML templating
- **Tailwind CSS**: Utility-first CSS framework
- **HTMX**: Dynamic HTML without JavaScript frameworks
- **Kubernetes Dynamic Client**: For interacting with Tinkerbell CRDs

## API Integration

The UI integrates with Tinkerbell by importing type definitions from:
```go
import "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
```

This ensures type safety and compatibility with the Tinkerbell API schemas for Hardware, Template, and Workflow resources.
