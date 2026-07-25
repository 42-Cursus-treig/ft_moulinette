#include "ft_btree.h"
#include <unistd.h>

void	btree_apply_by_level(t_btree *root,
			void (*applyf)(void *item, int current_level, int is_first_elem));
t_btree	*build_fixed_tree(void);
void	free_tree(t_btree *root);

static void	put_nbr(int n)
{
	char	buf[12];
	int		i;

	i = 0;
	if (n == 0)
		buf[i++] = '0';
	while (n > 0)
	{
		buf[i++] = '0' + (n % 10);
		n /= 10;
	}
	while (i--)
		write(1, &buf[i], 1);
}

static void	show(void *item, int level, int is_first)
{
	char	*s;
	int		i;

	s = (char *)item;
	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
	write(1, " ", 1);
	put_nbr(level);
	write(1, " ", 1);
	put_nbr(is_first);
	write(1, "\n", 1);
}

int	main(void)
{
	t_btree	*root;

	root = build_fixed_tree();
	btree_apply_by_level(root, &show);
	free_tree(root);
	return (0);
}
