# Login Form Autofill Compatibility

This document provides a reference for the patterns and attributes required to ensure the Arcane login form is correctly detected and autofilled by password managers like **Bitwarden**, **1Password**, and mobile-native services (iOS/Android).

## Core Requirements

To be recognized as a login form, the following attributes must be present:

### 1. The `<form>` Element
The form must have standard attributes that signal its purpose to browser heuristics.
- `id="login-form"`: Provides a stable anchor for vault mapping.
- `name="login"`: Traditional attribute used by older password manager heuristics.
- `method="post"`: Signals data submission.
- `action=""`: Ensures the form is valid and submittable (even when intercepted by JS).
- `autocomplete="on"`: Explicitly allows the browser to offer saved credentials.

### 2. Input Fields
Fields must use standard names and autocomplete hints.
- **Username**: `id="username"`, `name="username"`, `autocomplete="username"`.
- **Password**: `id="password"`, `name="password"`, `autocomplete="current-password"`.
- **Labels**: Use explicit `<label for="...">` associated with the input `id`.
- **ARIA Labels**: Use `aria-label` as a fallback hint for field identification.

## Structural Constraints

### Avoid Nested Roles
Avoid using `role="group"` on containers directly wrapping input fields (like `InputGroup.Root`). 
- **Reason**: Many password managers use a shallow DOM scan. If an input is nested inside an element with a complex ARIA role, it may be treated as a "custom widget" rather than a standard form field and skipped by the autofill scanner.

## References

- [Bitwarden: How to help Bitwarden find your fields](https://bitwarden.com/help/custom-fields/)
- [Google/web.dev: Sign-in form best practices](https://web.dev/articles/sign-in-form-best-practices)
- [MDN: The HTML autocomplete attribute](https://developer.mozilla.org/en-US/docs/Web/HTML/Attributes/autocomplete)
- [W3C: ARIA Group Role Guidelines](https://www.w3.org/WAI/ARIA/apg/patterns/group/)
