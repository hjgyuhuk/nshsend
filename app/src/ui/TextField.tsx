import { Text, View } from "nsh";
import { Input } from "nsh/base";

export interface TextFieldProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  masked?: boolean;
  multiline?: boolean;
  helperText?: string;
  errorText?: string;
  disabled?: boolean;
  testID?: string;
}

/** Labeled native text input styled with semantic theme tokens. */
export function TextField(props: TextFieldProps) {
  return (
    <View class="gap-1.5">
      <Text class="text-sm font-medium text-foreground">{props.label}</Text>
      <Input
        testID={props.testID}
        value={props.value}
        onChange={({ value }) => props.onChange(value)}
        placeholder={props.placeholder}
        masked={props.masked}
        multiline={props.multiline}
        disabled={props.disabled}
        class={`rounded-(--radius) border px-3 py-2 text-sm text-foreground bg-background placeholder:text-muted-foreground focus:border-ring disabled:opacity-50 ${
          props.errorText ? "border-destructive" : "border-input"
        } ${props.multiline ? "min-h-24" : ""}`}
      />
      <If when={props.errorText}>
        <Text class="text-xs text-destructive">{props.errorText}</Text>
      </If>
      <If when={!props.errorText && props.helperText}>
        <Text class="text-xs text-muted-foreground">{props.helperText}</Text>
      </If>
    </View>
  );
}
