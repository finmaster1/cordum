import safeRegex from "safe-regex2";

const MAX_PATTERN_LENGTH = 1024;
const MAX_SAMPLE_TEXT_LENGTH = 10 * 1024; // 10KB

interface RegexValidation {
  valid: boolean;
  error?: string;
}

/**
 * Validates a regex pattern for safety before execution.
 * Checks: length limit, safe-regex2 catastrophic backtracking detection,
 * and RegExp compilation.
 */
export function validateRegex(pattern: string): RegexValidation {
  if (!pattern || !pattern.trim()) {
    return { valid: false, error: "Pattern is required." };
  }

  if (pattern.length > MAX_PATTERN_LENGTH) {
    return {
      valid: false,
      error: `Pattern exceeds maximum length (${MAX_PATTERN_LENGTH} characters).`,
    };
  }

  // Check for catastrophic backtracking patterns (ReDoS)
  if (!safeRegex(pattern)) {
    return {
      valid: false,
      error: "Pattern rejected: potentially unsafe regex that could cause performance issues.",
    };
  }

  // Verify it compiles
  try {
    new RegExp(pattern);
  } catch {
    return { valid: false, error: "Invalid regex syntax." };
  }

  return { valid: true };
}

/**
 * Truncates sample text to the maximum allowed length for regex testing.
 */
export function truncateSampleText(text: string): string {
  if (text.length <= MAX_SAMPLE_TEXT_LENGTH) return text;
  return text.slice(0, MAX_SAMPLE_TEXT_LENGTH);
}

export { MAX_PATTERN_LENGTH, MAX_SAMPLE_TEXT_LENGTH };
