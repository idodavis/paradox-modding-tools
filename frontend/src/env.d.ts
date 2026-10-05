/// <reference types="vite/client" />

/**
 * Module declarations for the grammar files the IDE imports.
 */
declare module "*.tmLanguage.json" {
  const grammar: Record<string, unknown>;
  export default grammar;
}
