# ECS Database

A novel database system designed for real-time, multi-user applications that combines Entity-Component-System (ECS) architecture with declarative state machines, real-time subscriptions, and built-in conflict resolution.

## What Makes This Different?

Most databases store data in tables and rows. This database is built around **entities** (unique IDs), **components** (typed data attached to entities), and **systems** (declarative state machines that define business logic). This approach is particularly powerful for:

- **Real-time collaborative applications** - Multiple users editing the same content simultaneously
- **Interactive dashboards** - Live data updates across connected clients  
- **Game backends** - Player state, inventory, and world management
- **Content management systems** - Publishing workflows with complex approval processes
- **IoT state management** - Device state synchronization and conflict resolution

## Core Concepts

### Entity-Component-System (ECS)
```go
// An entity is just a unique identifier
entityID := "blog-post-123"

// Components are typed data attached to entities
components := []Component{
    Title{Value: "My Blog Post"},
    Content{Text: "This is the content..."},
    PublishState{Status: "draft"},
    Author{UserID: "user-456"},
}
```

### Declarative State Machines
Instead of writing imperative business logic, you define state machines in JSON:

```json
{
  "states": {
    "draft": {
      "on": {
        "SUBMIT": {
          "target": "reviewing",
          "guard": {">=": [{"var": "Content.wordCount"}, 100]},
          "actions": [{"updateComponent": "PublishState"}]
        }
      }
    }
  }
}
```

### Built-in Conflict Resolution
Components can use different conflict resolution strategies:
- **Last-write-wins** for simple data
- **CRDTs** for collaborative text editing
- **Custom strategies** for domain-specific logic (like merging player positions)

## Project Status

🚧 **Currently in Phase 1: Core ECS Database**

- [x] Basic project structure and CLI
- [x] SQLite storage backend  
- [x] Complete schema validation system
- [ ] Database migration system
- [ ] Entity and component CRUD operations
- [ ] Query system

### Upcoming Phases
- **Phase 2**: State machines and procedures
- **Phase 3**: Real-time subscriptions via WebSockets
- **Phase 4**: CRDT integration and offline support
- **Phase 5**: Authentication, authorization, and production readiness

## Quick Start

```bash
# Clone the repository
git clone https://github.com/tmbritton/ecs-db
cd ecs-db

# Build and run
make build
./bin/ecs-db --help
```

## Example Schema

Entities are defined by their component composition:

```json
{
  "components": {
    "Title": {
      "type": "string",
      "maxLength": 200,
      "conflictResolution": "last-write-wins"
    },
    "Content": {
      "type": "text", 
      "conflictResolution": "crdt"
    },
    "Author": {
      "type": "reference"
    }
  },
  "entitys": {
    "BlogPost": {
      "components": ["Title", "Content", "Author"],
    }
  }
}
```

## Why Go?

This project is built in Go for several key reasons:
- **Performance**: Excellent for concurrent real-time applications
- **Simplicity**: Clear, readable code that's easy to maintain
- **Deployment**: Single binary deployment with no runtime dependencies
- **Ecosystem**: Great libraries for databases, networking, and JSON processing

## Vision

The goal is to make building collaborative, real-time applications significantly easier by handling the complex parts (state synchronization, conflict resolution, business logic execution) at the database level, so developers can focus on building great user experiences.

## License

- **Open Source**: GNU Affero General Public License v3.0 for open source projects

## Getting Help

- **Issues**: Report bugs or request features via GitHub Issues
- **Discussions**: Ask questions or share ideas in GitHub Discussions
- **Documentation**: Detailed docs are being written as features are implemented

