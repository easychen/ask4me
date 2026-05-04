import React from "react";
import {
  and,
  GroupLayout,
  LayoutProps,
  optionIs,
  rankWith,
  uiTypeIs,
} from "@jsonforms/core";
import {
  ResolvedJsonFormsDispatch,
  withJsonFormsLayoutProps,
} from "@jsonforms/react";

export const collapsibleGroupTester = rankWith(
  5,
  and(uiTypeIs("Group"), optionIs("collapsible", true))
);

type CollapsibleOptions = {
  defaultOpen?: boolean;
  maxHeight?: number | string;
};

const Component = ({
  uischema,
  schema,
  path,
  enabled,
  visible,
  renderers,
  cells,
}: LayoutProps) => {
  if (visible === false) return null;

  const layout = uischema as GroupLayout;
  const opts = (layout.options || {}) as CollapsibleOptions;
  const defaultOpen = opts.defaultOpen === true;
  const maxHeight =
    typeof opts.maxHeight === "number" || typeof opts.maxHeight === "string"
      ? opts.maxHeight
      : undefined;

  const contentStyle: React.CSSProperties = {
    paddingTop: 8,
    ...(maxHeight !== undefined ? { maxHeight, overflow: "auto" } : {}),
  };

  return (
    <details
      className="jsonforms-collapsible-group"
      open={defaultOpen}
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
        {layout.label || ""}
      </summary>
      <div style={contentStyle}>
        {(layout.elements || []).map((child, idx) => (
          <ResolvedJsonFormsDispatch
            key={`${path || "root"}-collapsible-${idx}`}
            schema={schema}
            uischema={child}
            path={path}
            enabled={enabled}
            renderers={renderers}
            cells={cells}
          />
        ))}
      </div>
    </details>
  );
};

export const CollapsibleGroupRenderer = withJsonFormsLayoutProps(Component);
