import { Text, View } from "nsh";
import type { JSX } from "nsh/jsx-runtime";

export interface SurfaceCardProps {
  title?: string;
  subtitle?: string;
  children?: JSX.Element;
  class?: string;
}

/** Semantic card container using theme card/border tokens. */
export function SurfaceCard(props: SurfaceCardProps) {
  return (
    <View class={`rounded-(--radius) border border-border bg-card p-4 gap-3 ${props.class ?? ""}`}>
      <If when={props.title}>
        <View class="gap-1">
          <Text class="text-base font-semibold text-card-foreground">{props.title}</Text>
          <If when={props.subtitle}>
            <Text class="text-xs text-muted-foreground">{props.subtitle}</Text>
          </If>
        </View>
      </If>
      {props.children}
    </View>
  );
}
