---
name: react-patterns
description: Modern React patterns — hooks, state management, performance.
---
# React Patterns
Modern React development patterns:

1. **Component composition**: Prefer composition over inheritance. Use children props.
2. **Custom hooks**: Extract reusable logic into hooks (useXxx naming).
3. **State management**: useState for local, useReducer for complex, Context for shared.
4. **Performance**: useMemo for expensive computations, useCallback for stable callbacks, React.memo.
5. **Data fetching**: TanStack Query, SWR, or useEffect with cleanup.
6. **Error boundaries**: Wrap sections with error boundary components.
7. **TypeScript**: Type all props, state, and event handlers.
8. **File structure**: Co-locate components with their styles and tests.
