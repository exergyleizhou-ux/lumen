---
name: api-design
description: REST API design patterns — naming, pagination, errors, versioning.
---
# API Design
REST API design guidelines:

1. **Resource naming**: Plural nouns (/users, /orders), hierarchical (/users/:id/orders).
2. **HTTP methods**: GET (read), POST (create), PUT (replace), PATCH (partial update), DELETE (remove).
3. **Status codes**: 200 OK, 201 Created, 204 No Content, 400 Bad Request, 401 Unauthorized, 403 Forbidden, 404 Not Found, 409 Conflict, 422 Unprocessable, 500 Internal.
4. **Pagination**: Cursor-based for large datasets, offset/limit for small. Include Link headers.
5. **Filtering & sorting**: Query params (?status=active&sort=-created_at).
6. **Error format**: Consistent `{ error: { code, message, details[] } }`.
7. **Versioning**: URL prefix (/v1/) or Accept header.
