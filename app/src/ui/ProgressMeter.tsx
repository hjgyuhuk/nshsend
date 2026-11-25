import { Text, View } from "nsh";
import { Progress } from "nsh/base";

export interface ProgressMeterProps {
  label: string;
  bytesTransferred: number;
  totalBytes: number;
  detailText?: string;
}

/** Linear transfer progress bar with percentage and byte counter. */
export function ProgressMeter(props: ProgressMeterProps) {
  const percent = () => {
    if (props.totalBytes <= 0) return 0;
    return Math.min(100, Math.round((props.bytesTransferred / props.totalBytes) * 100));
  };

  return (
    <Progress
      value={percent()}
      accessibilityLabel={props.label}
      class="gap-2"
      trackClass="h-2 overflow-hidden rounded-full bg-muted"
      indicatorClass="h-full rounded-full bg-primary"
    >
      <View class="flex-row items-center justify-between gap-2">
        <Text class="flex-1 text-xs text-foreground line-clamp-1">
          {props.label}
        </Text>
        <Text class="text-xs text-muted-foreground">
          {props.detailText ? `${props.detailText} · ${percent()}%` : `${percent()}%`}
        </Text>
      </View>
    </Progress>
  );
}
