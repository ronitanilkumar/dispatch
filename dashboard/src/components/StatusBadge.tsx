import {
  CheckCircle2,
  CircleDashed,
  Loader2,
  RotateCw,
  XCircle,
} from "lucide-react";
import { Tag } from "./ui";
import type { JobStatus } from "@/lib/types";
import { priorityLabel } from "@/lib/types";

export function StatusBadge({ status }: { status: JobStatus }) {
  switch (status) {
    case "pending":
      return (
        <Tag>
          <CircleDashed size={12} />
          Pending
        </Tag>
      );
    case "in-flight":
      return (
        <Tag tone="blue">
          <Loader2 size={12} className="spin" />
          In-flight
        </Tag>
      );
    case "retrying":
      return (
        <Tag tone="warn">
          <RotateCw size={12} />
          Retrying
        </Tag>
      );
    case "succeeded":
      return (
        <Tag tone="ok">
          <CheckCircle2 size={12} />
          Succeeded
        </Tag>
      );
    case "failed":
      return (
        <Tag tone="danger">
          <XCircle size={12} />
          Failed
        </Tag>
      );
  }
}

export function PriorityBadge({ priority }: { priority: number }) {
  // High is the only priority worth colouring; Normal/Low stay neutral so the
  // table does not turn into a wall of competing accents.
  return (
    <Tag tone={priority === 0 ? "danger" : "neutral"}>
      {priorityLabel(priority)}
    </Tag>
  );
}
