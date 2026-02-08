<script lang="ts">
  import Icon from "@iconify/svelte";
</script>

<!-- TODO: Figure out how to feed this thing paths and build the tree dynamically -->
<ul class="menu menu-xs bg-base-200 rounded-box max-w-xs w-full">
  <li>
    <button>
      <Icon icon="mdi:file" class="h-4 w-4" />
      resume.pdf
    </button>
  </li>
  <li>
    <details open>
      <summary>
        <Icon icon="mdi:folder" class="h-4 w-4" />
        My Files
      </summary>
      <ul>
        <li>
          <button>
            <Icon icon="mdi:file" class="h-4 w-4" />
            Project-final.psd
          </button>
        </li>
        <li>
          <button>
            <Icon icon="mdi:folder" class="h-4 w-4" />
            Project-final-2.psd
          </button>
        </li>
        <li>
          <details open>
            <summary>
              <Icon icon="mdi:folder" class="h-4 w-4" />
              Images
            </summary>
            <ul>
              <li>
                <button>
                  <Icon icon="mdi:file" class="h-4 w-4" />
                  Screenshot1.png
                </button>
              </li>
              <li>
                <button>
                  <Icon icon="mdi:file" class="h-4 w-4" />
                  Screenshot2.png
                </button>
              </li>
              <li>
                <details open>
                  <summary>Others</summary>
                  <ul>
                    <li>
                      <button>
                        <Icon icon="mdi:file" class="h-4 w-4" />
                        Screenshot3.png
                      </button>
                    </li>
                  </ul>
                </details>
              </li>
            </ul>
          </details>
        </li>
      </ul>
    </details>
  </li>
  <li>
    <button>
      <Icon icon="mdi:file" class="h-4 w-4" />
      reports-final-2.pdf
    </button>
  </li>
</ul>
