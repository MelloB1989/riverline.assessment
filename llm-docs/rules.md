# Backend Development Rules

## Database Model Conventions

### Struct Definition Rules

All database models must follow these conventions for Karma ORM compatibility:

```go
type User struct {
    TableName   string        `karma_table:"users" json:"-"`
    Id          string        `json:"id" karma:"primary"`
    FirstName   string        `json:"first_name"`
    Email       string        `json:"email"`
    Hobbies     []string      `json:"hobbies" db:"hobbies"`
    Metadata    ExtraMetadata `json:"metadata" db:"metadata"`
    CreatedAt   time.Time     `json:"created_at"`
}
```

### Field Naming

| Go Field | JSON Tag | Description |
|----------|----------|-------------|
| `CamelCase` | `snake_case` | JSON tags are used as database column names |
| `Id` | `"id"` | Primary key field |
| `FirstName` | `"first_name"` | Regular string field |
| `IsActive` | `"is_active"` | Boolean field |

### Tag Usage

| Tag | When to Use |
|-----|-------------|
| `karma_table:"table_name"` | Required on `TableName` field for table mapping |
| `karma:"primary"` | Required on primary key field |
| `json:"column_name"` | Required on all fields (used as DB column name) |
| `db:"column_name"` | **Only for complex types** (arrays, slices, maps, structs/JSONB) |

### When to Use `db` Tag

Add `db:"column_name"` tag **only** for:
- `[]string` (PostgreSQL arrays)
- `[]StructType` (JSONB arrays)
- `map[string]any` (JSONB objects)
- Nested structs (JSONB)

```go
// ✅ Correct - db tag for complex types only
Hobbies  []string      `json:"hobbies" db:"hobbies"`
Extra    ExtraMetadata `json:"extra" db:"extra"`
Messages []Message     `json:"messages" db:"messages"`

// ✅ Correct - no db tag for simple types
Id        string    `json:"id" karma:"primary"`
Name      string    `json:"name"`
IsActive  bool      `json:"is_active"`
CreatedAt time.Time `json:"created_at"`

// ❌ Wrong - unnecessary db tag on simple types
Name string `json:"name" db:"name"`
```

---

## Model Location

All database models **must** be defined in:
```
services/server/internal/models/schema.go
```

---

## ORM Usage

### Redis Caching

**Do NOT** manually manage Redis clients for ORM caching. The ORM handles Redis internally:

```go
// ✅ Correct - ORM handles Redis
func workflowORM() *orm.ORM {
    return orm.Load(&workflowRecord{}, 
        orm.WithCacheOn(true),
        orm.WithCacheMethod("redis"),
        orm.WithCacheKey("prefix"),
        orm.WithCacheTTL(10*time.Minute),
    )
}

// ❌ Wrong - manually managing Redis client
type Store struct {
    redisClient *redis.Client
}
```

### Field Lookup

Use **Go field names** (CamelCase) in `GetByFieldEquals`:

```go
// ✅ Correct
orm.GetByFieldEquals("OwnerId", ownerID)
orm.GetByFieldEquals("Id", id)

// ❌ Wrong - using snake_case
orm.GetByFieldEquals("owner_id", ownerID)
```

---

## Common Mistakes to Avoid

1. **Don't add `db` tag to simple types** (string, bool, int, time.Time)
2. **Don't manage Redis clients manually** for ORM caching
3. **Don't use snake_case in `GetByFieldEquals`** - use CamelCase Go field names
4. **Don't define DB models outside schema.go**
5. **Don't mix JSON casing** - always use snake_case for DB models' JSON tags
