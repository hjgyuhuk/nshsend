import { Text, View } from "nsh";

export type StatusBadgeTone = "neutral" | "primary" | "success" | "warning" | "destructive";

export interface StatusBadgeProps {
  label: string;
  tone?: StatusBadgeTone;
}

const TONE_BOX_CLASS: Record<StatusBadgeTone, string> = {
  neutral: "bg-secondary",
  primary: "bg-accent",
  success: "bg-success",
  warning: "bg-warning",
  destructive: "bg-destructive",
};

const TONE_TEXT_CLASS: Record<StatusBadgeTone, string> = {
  neutral: "text-secondary-foreground",
  primary: "text-accent-foreground",
  success: "text-success-foreground",
  warning: "text-warning-foreground",
  destructive: "text-destructive-foreground",
};

/** Compact semantic status badge. */
export function StatusBadge(props: StatusBadgeProps) {
  const tone = () => props.tone ?? "neutral";
  return (
    <View class={`self-start rounded-(--radius) px-2 py-0.5 ${TONE_BOX_CLASS[tone()]}`}>
      <Text class={`text-xs font-medium ${TONE_TEXT_CLASS[tone()]}`}>{props.label}</Text>
    </View>
  );
}
