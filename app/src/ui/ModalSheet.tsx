import { Text, View } from "nsh";
import type { JSX } from "nsh/jsx-runtime";
import { Dialog } from "nsh/base";

export interface ModalSheetProps {
  open: boolean;
  title: string;
  description?: string;
  onClose: () => void;
  children?: JSX.Element;
  actions?: JSX.Element;
}

/** Centered desktop modal dialog built on nsh/base Dialog. */
export function ModalSheet(props: ModalSheetProps) {
  return (
    <Dialog
      open={props.open}
      onOpenChange={(e) => {
        if (!e.open) props.onClose();
      }}
      backdropClass="bg-foreground/40"
      class="m-auto w-full max-w-md rounded-(--radius) border border-border bg-card p-6 gap-4"
    >
      <View class="gap-1">
        <Text class="text-lg font-semibold text-card-foreground">{props.title}</Text>
        <If when={props.description}>
          <Text class="text-sm text-muted-foreground">{props.description}</Text>
        </If>
      </View>
      {props.children}
      <If when={props.actions}>
        <View class="flex-row justify-end gap-2 pt-2">{props.actions}</View>
      </If>
    </Dialog>
  );
}
