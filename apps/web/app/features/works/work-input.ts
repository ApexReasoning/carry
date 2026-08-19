export const maxGoalBytes = 2_000;
export const maxMessageBytes = 60 * 1_024;

const encoder = new TextEncoder();

export function goalInputError(value: string): string | null {
  const normalized = value.trim();
  if (!normalized) return "Enter a goal for this Work.";
  if (encoder.encode(normalized).byteLength > maxGoalBytes) {
    return `Keep the goal within ${maxGoalBytes} UTF-8 bytes.`;
  }
  return null;
}

export function messageInputError(value: string): string | null {
  if (!value.trim()) return "Enter information for Carry.";
  if (encoder.encode(value).byteLength > maxMessageBytes) {
    return `Keep the message within ${maxMessageBytes} UTF-8 bytes.`;
  }
  return null;
}
