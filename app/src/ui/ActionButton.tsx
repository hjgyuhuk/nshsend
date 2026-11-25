import { Text } from "nsh";
import { Button } from "nsh/base";
import { t } from "nsh/i18n";

export type ActionButtonVariant = "primary" | "secondary" | "outline" | "ghost" | "destructive";
export type ActionButtonSize = "sm" | "md";

export interface ActionButtonProps {
  label: string;
  onPress: () => void;
  variant?: ActionButtonVariant;
  size?: ActionButtonSize;
  disabled?: boolean;
  loading?: boolean;
  testID?: string;
}

const VARIANT_BOX_CLASS: Record<ActionButtonVariant, string> = {
  primary: "bg-primary hover:bg-primary/90 active:bg-primary/80",
  secondary: "bg-secondary hover:bg-secondary/80 active:bg-secondary/70",
  outline: "border border-border bg-card hover:bg-secondary active:bg-muted",
  ghost: "bg-transparent hover:bg-secondary active:bg-muted",
  destructive: "bg-destructive hover:bg-destructive/90 active:bg-destructive/80",
};

const VARIANT_TEXT_CLASS: Record<ActionButtonVariant, string> = {
  primary: "text-primary-foreground",
  secondary: "text-secondary-foreground",
  outline: "text-foreground",
  ghost: "text-foreground",
  destructive: "text-destructive-foreground",
};

const SIZE_BOX_CLASS: Record<ActionButtonSize, string> = {
  sm: "px-3 py-1.5 rounded-(--radius)",
  md: "px-4 py-2 rounded-(--radius)",
};

/** Styled button wrapping nsh/base Button with semantic variants and loading/disabled states. */
export function ActionButton(props: ActionButtonProps) {
  const variant = () => props.variant ?? "primary";
  const size = () => props.size ?? "md";
  const isDisabled = () => Boolean(props.disabled || props.loading);

  return (
    <Button
      testID={props.testID}
      disabled={isDisabled()}
      onPress={props.onPress}
      class={`items-center justify-center disabled:opacity-50 focus-visible:border-ring ${SIZE_BOX_CLASS[size()]} ${VARIANT_BOX_CLASS[variant()]}`}
    >
      <Text class={`text-sm font-medium ${VARIANT_TEXT_CLASS[variant()]}`}>
        {props.loading ? t("common.loading") : props.label}
      </Text>
    </Button>
  );
}
