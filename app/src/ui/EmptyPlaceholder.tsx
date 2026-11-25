import { Text, View } from "nsh";
import type { JSX } from "nsh/jsx-runtime";

export interface EmptyPlaceholderProps {
  title: string;
  description?: string;
  action?: JSX.Element;
}

/** Consistent empty/error placeholder block. */
export function EmptyPlaceholder(props: EmptyPlaceholderProps) {
  return (
    <View class="items-center justify-center rounded-(--radius) border border-border bg-card p-8 gap-3">
      <Text class="text-sm font-medium text-foreground">{props.title}</Text>
      <If when={props.description}>
        <Text class="text-xs text-muted-foreground">{props.description}</Text>
      </If>
      <If when={props.action}>{props.action}</If>
    </View>
  );
}
