<script lang="ts">
  import logoIcon from "@assets/PMT-Logo-Icon.svg?url";
  import logoText from "@assets/PMT-Logo-Text.svg?url";

  let {
    class: className = "",
    iconHeight = 30,
    textHeight = 40,
  } = $props<{ class?: string; iconHeight?: number; textHeight?: number }>();
</script>

<!-- SVG as mask: shape from file, color from accent (dynamic) -->
<div class="inline-flex items-center {className}">
  <span
    class="inline-block"
    style="
      width: calc({iconHeight}px * 2);
      height: {iconHeight}px;
      background-color: var(--color-accent);
      mask-image: url('{logoIcon}');
      mask-size: contain;
      mask-repeat: no-repeat;
      mask-position: center;
    "
  ></span>
  <span
    class="inline-block"
    style="
      width: calc({textHeight}px * 3);
      height: {textHeight}px;
      background-color: var(--color-secondary);
      mask-image: url('{logoText}');
      mask-size: contain;
      mask-repeat: no-repeat;
      mask-position: 0% center;
      margin-left: calc(-0.9 * {iconHeight}px);
    "
  ></span>
</div>
