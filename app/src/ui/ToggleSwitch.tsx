import { Text, View } from "nsh";
import { Switch } from "nsh/base";

export interface ToggleSwitchProps {
  label: string;
  description?: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  testID?: string;
}

/** Controlled switch with label and optional description. */
export function ToggleSwitch(props: ToggleSwitchProps) {
  return (
    <Switch
      testID={props.testID}
      checked={props.checked}
      disabled={props.disabled}
      onChange={({ checked }) => props.onChange(checked)}
      class="flex-row items-center justify-between gap-4 disabled:opacity-50"
      trackClass="w-11 rounded-full bg-muted p-0.5 transition-colors checked:bg-primary"
      thumbClass="size-5 rounded-full bg-background shadow-sm transition-transform checked:translate-x-5"
    >
      <View class="flex-1 gap-0.5">
        <Text class="text-sm font-medium text-foreground">{props.label}</Text>
        <If when={props.description}>
          <Text class="text-xs text-muted-foreground">{props.description}</Text>
        </If>
      </View>
    </Switch>
  );
}
