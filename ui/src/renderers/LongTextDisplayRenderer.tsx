import React from "react";
import {
  and,
  ControlProps,
  optionIs,
  rankWith,
  uiTypeIs,
} from "@jsonforms/core";
import { withJsonFormsControlProps } from "@jsonforms/react";

export const longTextDisplayTester = rankWith(
  6,
  and(uiTypeIs("Control"), optionIs("readonlyBlock", true))
);

type LongTextDisplayOptions = {
  collapsible?: boolean;
  defaultOpen?: boolean;
  maxHeight?: number | string;
  summary?: string;
};

const Component = ({ uischema, data, label, visible }: ControlProps) => {
  if (visible === false) return null;

  const opts = ((uischema && (uischema as any).options) ||
    {}) as LongTextDisplayOptions;
  const text = data == null ? "" : String(data);
  const maxHeight =
    typeof opts.maxHeight === "number" || typeof opts.maxHeight === "string"
      ? opts.maxHeight
      : undefined;

  const blockStyle: React.CSSProperties = {
    whiteSpace: "pre-wrap",
    lineHeight: 1.6,
    color: "#374151",
    border: "1px solid #e5e7eb",
    borderRadius: 4,
    padding: 8,
    background: "#fafafa",
    ...(maxHeight !== undefined ? { maxHeight, overflow: "auto" } : {}),
  };

  const body = <div style={blockStyle}>{text}</div>;

  if (opts.collapsible) {
    const summaryText = opts.summary ?? label ?? "详情";
    return (
      <details
        className="jsonforms-collapsible-control"
        open={opts.defaultOpen === true}
        style={{
          border: "1px solid #e5e7eb",
          borderRadius: 6,
          padding: "6px 10px",
          margin: "8px 0",
        }}
      >
        <summary
          style={{
            cursor: "pointer",
            fontWeight: 600,
            padding: "4px 0",
            userSelect: "none",
          }}
        >
          {summaryText}
        </summary>
        <div style={{ paddingTop: 8 }}>{body}</div>
      </details>
    );
  }

  return (
    <div className="jsonforms-long-text-display" style={{ margin: "8px 0" }}>
      {label ? (
        <div style={{ fontWeight: 600, marginBottom: 4 }}>{label}</div>
      ) : null}
      {body}
    </div>
  );
};

export const LongTextDisplayRenderer = withJsonFormsControlProps(Component);
