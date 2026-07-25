#include "ft_btree.h"
#include <unistd.h>

void	btree_apply_infix(t_btree *root, void (*applyf)(void *));
t_btree	*build_fixed_tree(void);
void	free_tree(t_btree *root);

static void	print_item(void *item)
{
	char	*s;
	int		i;

	s = (char *)item;
	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
	write(1, " ", 1);
}

int	main(void)
{
	t_btree	*root;

	root = build_fixed_tree();
	btree_apply_infix(root, &print_item);
	write(1, "\n", 1);
	free_tree(root);
	return (0);
}
