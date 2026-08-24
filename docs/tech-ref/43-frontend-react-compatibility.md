# Frontend React compatibility and warning policy

## Current decision

- React Router 6 runs with `v7_startTransition` and `v7_relativeSplatPath` enabled in production and in the shared test router.
- `React.StrictMode` stays enabled. Console filtering and warning suppression are not accepted fixes.
- UniRouter-owned frontend source has no `findDOMNode` call.
- React 19 is blocked until the temporary Semi UI waiver below is retired.

## FE-REACT18-SEMI-FINDDOMNODE-001

Status: temporary dependency waiver, React 18 only.

Verified on 2026-08-24 with React 18.3.1 and `@douyinfe/semi-ui` 2.100.0. The observed development/test stacks originate in Semi UI, including:

- `ReactResizeObserver → Typography.Base/Text → Table`;
- `Tooltip → Button/IconButton`, including the password visibility control.

The warning is not emitted by UniRouter-owned source. Upgrading Semi alone is not currently a fix: the official Semi 2.102.0 source still selects `ReactDOM.findDOMNode` for React 16/17/18 and uses its React 19 fallback only when that API is absent. `react-transition-group` is also present through `Recharts → react-smooth`, but it has not been established as the source of the observed UniRouter warning and is not included in this waiver without a captured runtime stack.

This waiver does not suppress the warning. It records why the React 18 release is not blocked while preserving the warning as visible upgrade debt. `npm run check:react-compat` fails if StrictMode or the router contract is removed, UniRouter source adds `findDOMNode`, React is raised to 19, or Semi changes without revalidation.

## Waiver exit criteria

Before upgrading to React 19, all of the following must be true:

1. Captured development and test stacks contain no `findDOMNode` warning.
2. Semi UI uses its supported React 19 adapter and no application trigger relies on a class-component DOM lookup.
3. Tooltip, Popover/Popconfirm, Modal, Select, table ellipsis/resize behavior, and chart animation pass focused interaction tests.
4. Navigation, refresh deep links, absolute redirects, and catch-all routing pass with the future behavior enabled.
5. This waiver and the React-major block are removed in the same reviewed change.

## Primary references

- React Router future flags and v7 adopted behavior: <https://reactrouter.com/changelog>
- React removal of `ReactDOM.findDOMNode`: <https://github.com/facebook/react/blob/main/CHANGELOG.md>
- Semi React 19 adaptation guide: <https://semi.design/en-US/ecosystem/react19>
- Semi 2.102.0 React compatibility implementation: <https://github.com/DouyinFE/semi-design/blob/v2.102.0/packages/semi-ui/_utils/reactRender.ts>
